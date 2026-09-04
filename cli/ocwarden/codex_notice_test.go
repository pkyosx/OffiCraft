package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// THE SAME OWNER RULING, ON THE OTHER RUNTIME (2026-08-30):
//
//	「應該是在第一次斷線，跟連線回來的時候發訊息給 agent，中間的 retry 我們不需要
//	 降低頻率，但是不需要打攪 agent。」
//
// The two runtimes sat at OPPOSITE extremes of it and neither was what he asked
// for. A claude member reads ocagent's stdout directly, so EVERY transport line
// was an interruption (too loud). A codex member reads it through this sidecar,
// whose forwarding filter drops every line beginning `[ocagent] listen:` — so a
// codex member was told about its transport EXACTLY ONCE, at boot, and every
// disconnect and every reconnect after that was silent (too quiet).
//
// The middle — the thing he actually asked for — is: forward the two endpoint
// notices and the give-up line, and nothing else.
//
// 🔴 WHAT MUST SURVIVE THIS: the once-per-session post-boot wake (T-51b0). It is
// the only thing that continues a codex boot after SSE comes up, it fires on the
// same `connected` prefix these notices now ride, and when it is missing nothing
// errors and nothing reports it — the agent simply never starts, which looks
// exactly like an agent with nothing to do. The pre-existing tests in
// codex_session_test.go pin it; this file must not be allowed to weaken them.
// ---------------------------------------------------------------------------

const connectedLineFixture = "[ocagent] listen: connected — streaming http://127.0.0.1 " +
	"(⇒ online while held) [same station] [station abc123]"

func TestCodexForwardsOnlyTheTwoEndpointNoticesAndTheGiveUp(t *testing.T) {
	for line, want := range map[string]bool{
		// The endpoints the owner named, plus the give-up line that keeps
		// silence unambiguous. These MUST reach the model.
		"[ocagent] listen: disconnected — connect failed: unexpected status 502": true,
		"[ocagent] listen: giving up — context cancelled":                        true,
		connectedLineFixture: true,
		// Everything between the endpoints stays out of the transcript.
		"[ocagent] listen: connect failed: unexpected status 502": false,
		"[ocagent] listen: stream ended: EOF":                     false,
		"[ocagent] listen: connect refused: 409":                  false,
		// Negative control: ordinary events were always forwarded and still are,
		// so the assertions above are about these lines and not about all lines.
		"[ocagent] chat from owner (#CM-9F2A11, 1s ago): hello": true,
		"[ocagent] task T-1 updated · by owner":                 true,
	} {
		if got := actionableCodexListenerLine(line); got != want {
			t.Errorf("%q: forwarded=%v want %v", line, got, want)
		}
	}
}

// The boot connect must NOT arrive twice. It opens the once-only post-boot wake,
// and now that the connected line is also a forwardable notice, the naive
// composition would open two turns for one event.
func TestCodexBootConnectWakesOnceAndIsNotAlsoForwarded(t *testing.T) {
	var turns []string
	st := &codexListenerState{}
	feed := func(line string) {
		st.handleListenerLine(line, func() {}, func(text string) { turns = append(turns, text) }, nil)
	}

	feed(connectedLineFixture)
	if len(turns) != 1 || turns[0] != codexPostBootWake {
		t.Fatalf("the boot connect must open exactly the post-boot wake and nothing "+
			"else; turns=%q", turns)
	}

	// A RECONNECT is the owner's second endpoint. It must reach the agent — as
	// the notice, never as a second boot wake.
	feed(connectedLineFixture)
	if len(turns) != 2 {
		t.Fatalf("a reconnect must tell the agent it is back; turns=%q", turns)
	}
	if turns[1] == codexPostBootWake {
		t.Fatalf("a reconnect re-opened the BOOT wake; that boot is long over and the "+
			"turn interrupts real work. turns=%q", turns)
	}
	if turns[1] != connectedLineFixture {
		t.Fatalf("the reconnect notice must be the listener's own line; turns=%q", turns)
	}

	feed("[ocagent] listen: disconnected — connect failed: unexpected status 502")
	feed("[ocagent] listen: connect failed: unexpected status 502") // a mid-outage retry
	feed("[ocagent] listen: giving up — context cancelled")
	if len(turns) != 4 {
		t.Fatalf("one disconnect + one give-up must land and the mid-outage retry must "+
			"not; turns=%q", turns)
	}
}

// 🔴 THE DELIVERY, NOT THE DECISION. Everything above this line asks what the
// sidecar DECIDED to do with a line. Independent review showed that is not the
// same question as whether anything happened: it replaced the send at the end of
// the delivery path — `s.steerOrStart(text)` → `_ = text` — inside
// runCodexSession's real select loop, and the entire ocwarden suite plus
// bin/uplink-guard.py stayed green while EVERY forwarded notice and every
// chat/task event stopped reaching the codex model. A member would sit through a
// station changeover, and through its own chat, with nothing arriving and
// nothing anywhere reporting it.
//
// So this test does not record turns into a slice of its own. It builds a real
// codexSession, hands the loop's real openTurn (s.openListenerTurn) to the real
// decision seam, and reads the App Server bytes that came out the other end.
func TestListenerNoticesReallyReachTheModel(t *testing.T) {
	const disconnected = "[ocagent] listen: disconnected — connect failed: unexpected status 502"
	const givingUp = "[ocagent] listen: giving up — context cancelled"

	wire := &bufferWriteCloser{}
	pane := &bytes.Buffer{}
	session := &codexSession{in: wire, threadID: "th-1", effort: "medium", out: pane}
	st := &codexListenerState{}
	feed := func(line string) {
		st.handleListenerLine(line, func() {}, session.openListenerTurn, session.closeBatch)
	}

	feed(connectedLineFixture) // boot: the post-boot wake is the turn that goes out
	feed(disconnected)
	feed(connectedLineFixture) // the reconnect notice
	feed(givingUp)
	feed("[ocagent] listen: stream ended: EOF") // mid-outage chatter: must not go out
	feed("[ocagent] chat from owner (#CM-9F2A11, 1s ago): hello")

	var sent []string
	for _, raw := range strings.Split(strings.TrimSpace(wire.String()), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			t.Fatalf("the sidecar wrote something the App Server cannot read: %v\n%s", err, raw)
		}
		method, _ := msg["method"].(string)
		if method != "turn/start" && method != "turn/steer" {
			continue
		}
		params, _ := msg["params"].(map[string]any)
		input, _ := params["input"].([]any)
		if len(input) != 1 {
			t.Fatalf("turn %q carried %d input items, want 1: %s", method, len(input), raw)
		}
		item, _ := input[0].(map[string]any)
		text, _ := item["text"].(string)
		sent = append(sent, text)
	}

	want := []string{
		codexPostBootWake, // boot connect
		disconnected,      // the first failure of the outage
		connectedLineFixture,
		givingUp,
		"[ocagent] chat from owner (#CM-9F2A11, 1s ago): hello",
	}
	if len(sent) != len(want) {
		t.Fatalf("the model received %d turns, want %d — a notice that is decided on "+
			"but never sent is a member sitting in silence with nothing to report it.\n"+
			"sent=%q\nwant=%q\nwire:\n%s", len(sent), len(want), sent, want, wire.String())
	}
	for i := range want {
		if sent[i] != want[i] {
			t.Errorf("turn %d text = %q, want %q", i, sent[i], want[i])
		}
	}

	// The steer half of the same seam: a notice arriving DURING a live turn must
	// still reach the model, as a steer rather than a new turn.
	wire.Reset()
	session.active = true
	session.turnID = "turn-9"
	session.openListenerTurn(disconnected)
	if !strings.Contains(wire.String(), `"turn/steer"`) ||
		!strings.Contains(wire.String(), "listen: disconnected") {
		t.Fatalf("a notice that arrives mid-turn must steer the live turn; wire:\n%s",
			wire.String())
	}
}
