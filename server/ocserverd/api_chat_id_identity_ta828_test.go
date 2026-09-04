package main

// api_chat_id_identity_ta828_test.go — THE THREE PLACES ARE ONE ID (T-a828).
//
// The promise this ticket ships is a chain of three hops, and each hop is a
// DIFFERENT code path — two of them in a different Go module:
//
//	① the wake snapshot folds a message and stamps it with an `id`
//	   (resumeChatMessageDTO → chatMessageDTO.ID)
//	② the ocagent notification line prints `#<that id>`
//	   (cli/ocagent/listen.go drainChat, reading a field out of the chat payload)
//	③ get_chat's new `ids` takes that value back and returns the whole body
//	   (serveChatByIDs)
//
// Every other test on this surface checks ONE of those in isolation, so the
// sentence a waking agent is actually told — "take the id off the notification
// line and hand it to get_chat" — is pinned by nothing. Change the source of
// any one hop and it fails SILENTLY: the snapshot still has an id, the line
// still prints a `#…`, and get_chat still answers about SOMETHING.
//
// So this file takes ONE REAL MESSAGE and pulls the value out of all three
// places, then compares them. It does not check three formats separately and
// it does not eyeball anything.
//
// ── what is covered, and what is not ────────────────────────────────────────
//
// ① and ③ are covered END TO END, over real HTTP through the real mux: the id
// is READ OUT of a real resume_summary response and HANDED to a real
// `?ids=` request, and the message that comes back must be the same one, whole.
//
// ② is covered as a PAYLOAD CONTRACT, not as an execution. `cli/ocagent` is a
// separate Go module (root CLAUDE.md §13: the four modules cannot import each
// other), so drainChat cannot be run from here. What is checked instead is the
// only thing that can drift between them: the FIELD NAME. The key is derived
// from the CLI source itself — parsed, not transcribed — by asking "which map
// key's value is the one printed after the `#`", and the value under THAT key
// in the server's own chat payload must be the same id as ①/③.
//
//	NOT covered by this file: that drainChat renders the tag at all, that it
//	renders it as `#<id>` rather than some other shape, its dedupe/`seen`
//	behaviour, and everything about the ocagent side that is not the field
//	name. Those live in cli/ocagent's own tests. If someone stops printing the
//	tag entirely, this file stays green.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ocagentListenSource is the CLI file that renders the notification line. It is
// read, not imported: the two live in different Go modules.
const ocagentListenSource = "../../cli/ocagent/listen.go"

// TestChatIDs_TheSnapshotTheNotificationAndTheReReadNameTheSameMessage is the
// whole point of this file. One message, three extractions, one comparison.
func TestChatIDs_TheSnapshotTheNotificationAndTheReReadNameTheSameMessage(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)

	// A message from ANOTHER agent, long enough that the snapshot folds it —
	// the folded message is the only kind whose id a reader ever needs.
	const wholeBody = "the whole plan, at length: " +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa " +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb " +
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc END-OF-PLAN"
	const wantID = "c-chain-1"
	if err := dal.PutChat(ChatMessage{
		ID: wantID, Sender: "m-1", Recipient: "m-2",
		Body: wholeBody, TS: float64(time.Now().Unix() - 60),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	get := func(path string) []byte {
		t.Helper()
		tok, err := mintJWT("m-2", "agent", 3600, secret, time.Now().Unix(), "")
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		req, err := http.NewRequest("GET", srv.URL+path, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s → %d: %s", path, resp.StatusCode, body)
		}
		return body
	}

	// ── ① the id the WAKE SNAPSHOT stamps on the folded message ──────────────
	var snap struct {
		Chat []map[string]any `json:"chat"`
	}
	if err := json.Unmarshal(get("/api/resume-summary"), &snap); err != nil {
		t.Fatalf("decode resume_summary: %v", err)
	}
	var folded map[string]any
	for _, m := range snap.Chat {
		if n, _ := m["body_omitted_chars"].(float64); n > 0 {
			folded = m
			break
		}
	}
	if folded == nil {
		// Precondition, not the assertion: with nothing folded, the sentence
		// this file is about ("re-read it with get_chat") is never spoken, and
		// comparing ids would prove nothing.
		t.Fatalf("precondition: the snapshot folded nothing, so there is no "+
			"re-read to chain: %v", snap.Chat)
	}
	fromSnapshot, _ := folded["id"].(string)
	if fromSnapshot == "" {
		t.Fatalf("the folded message carries no id — the fold marker points at "+
			"a door with no handle: %v", folded)
	}

	// ── ② the field the NOTIFICATION LINE reads, taken from the CLI source ───
	key := notificationIDField(t)
	var served []map[string]any
	if err := json.Unmarshal(chatEnvelopeMessages(t, get("/api/chat?with=m-2")), &served); err != nil {
		t.Fatalf("decode chat: %v", err)
	}
	var row map[string]any
	for _, m := range served {
		// Located by BODY, deliberately: locating it by id would make this
		// test agree with itself.
		if b, _ := m["body"].(string); b == wholeBody {
			row = m
			break
		}
	}
	if row == nil {
		t.Fatalf("precondition: the message is not on the very payload ocagent "+
			"fetches (GET /api/chat?with=<self>): %s", get("/api/chat?with=m-2"))
	}
	fromNotification, _ := row[key].(string)
	if fromNotification == "" {
		t.Fatalf("the chat payload carries nothing under %q — that is the field "+
			"drainChat prints after the '#', so the notification line would "+
			"name no message at all: %v", key, row)
	}

	// ── ③ the id get_chat's `ids` accepts, and what it hands back ────────────
	var reread []struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(chatEnvelopeMessages(t, get("/api/chat?ids="+fromSnapshot)), &reread); err != nil {
		t.Fatalf("decode by-id read: %v", err)
	}
	if len(reread) != 1 {
		t.Fatalf("the id taken off the snapshot must name exactly that one "+
			"message, got %d", len(reread))
	}
	fromReRead := reread[0].ID

	// ── the comparison this file exists for ─────────────────────────────────
	if fromSnapshot != fromNotification || fromSnapshot != fromReRead {
		t.Fatalf("the three places have drifted — the promise 'take the id off "+
			"the notification and hand it to get_chat' is silently broken:\n"+
			"  resume_summary chat[].id          = %q\n"+
			"  chat payload[%q] (drainChat reads) = %q\n"+
			"  get_chat ?ids= answered about      = %q",
			fromSnapshot, key, fromNotification, fromReRead)
	}
	if fromSnapshot != wantID {
		t.Fatalf("all three agree on %q, but that is not the message that was "+
			"written (%q) — agreeing on the wrong thing is not agreement",
			fromSnapshot, wantID)
	}
	// And the re-read has to be worth doing: the snapshot showed a stump, the
	// re-read must give the text back.
	if reread[0].Body != wholeBody {
		t.Fatalf("the by-id re-read must return the WHOLE body — that is what "+
			"the fold marker promises: %q", reread[0].Body)
	}
	if b, _ := folded["body"].(string); b == wholeBody {
		t.Fatalf("precondition: the snapshot did not actually shorten anything, " +
			"so 'the re-read gives it back' is vacuously true")
	}
}

// notificationPrinters names the ocagent functions allowed to emit the "#" tag
// on a chat notification line. Anything printing it from somewhere else is out
// of contract; add it here deliberately, with a reason, rather than widening the
// search to the whole file (see notificationIDField for why that is fail-open).
var notificationPrinters = map[string]bool{
	"drainChat":     true, // printed here until T-bb78
	"printChatLine": true, // the helper drainChat calls per message since T-bb78
}

// notificationIDField derives, from the ocagent source itself, the chat-payload
// field whose value is printed after the "#" on the notification line.
//
// It is DERIVED rather than written down here because a constant would be a
// second copy of the very thing this file is checking: someone renaming the
// field on the CLI side would edit their copy and leave ours agreeing with a
// server that no longer matches them.
//
// The derivation is structural: find `"#" + <ident>` inside a NAMED printer
// (see notificationPrinters above), then find where that ident was assigned
// INSIDE THE SAME FUNCTION, and read the single string-literal map key in that
// assignment. Anything it cannot resolve is a FATAL — the shape of the line
// changed, and a human has to re-check this contract rather than have the test
// quietly widen.
//
// The search is scoped to notificationPrinters, a NAMED list of the functions
// that may emit that line. It used to say drainChat and nothing else, and T-bb78
// moved the printing into a printChatLine helper without changing one byte of
// what is printed — so a red here can mean "re-point this list" rather than "the
// contract broke", and the list is where you re-point it.
//
// The list is not decoration. Searching the WHOLE FILE instead was tried and is
// FAIL-OPEN: with the real print writing something other than a bare identifier
// (say `"#"+strOrEmpty(m["reply_to"])`) and ANY unrelated `"#" + ident` left
// elsewhere in the file, the guard finds the decoy, derives a field nobody
// prints, and passes green while the notification names the wrong message. An
// independent review demonstrated exactly that mutant. Scoped to named printers,
// the same mutant resolves nothing and this FATALs instead — the shape changed,
// so a human re-checks it.
//
// THERE IS NO ESCAPE HATCH, ON PURPOSE, AND YOU WILL MEET IT BEFORE YOU MEET
// THIS COMMENT. Across ALL the named printers together there may be AT MOST ONE
// `"#" + <bare local>` and NO `"#" + <anything else>` — three separate FATALs,
// and it is worth knowing all three because the third one is the one that
// surprises people: >1 bare local in one printer, ANY opaque form in one
// printer, and a bare local in TWO named printers. So adding a SECOND
// "#"-prefixed tag INSIDE ONE OF THEM — a thread tag, a task tag — FATALs in
// every spelling there is. That is not an oversight to be relaxed away: all
// three branches are the
// fix for a fail-open an independent review actually demonstrated (a real print
// of the WRONG field passing green beside a never-printed decoy), and widening
// any of them back puts that hole straight back.
//
// Two ways through, both demonstrated green by an independent review, and note
// what each one does NOT solve:
//
//   - Teach this guard which of the tags is the message-id one. Solves the >1
//     bare local branch only. It CANNOT be used to relax the opaque branch —
//     that is where the demonstrated mutant lives.
//   - Build the NEW tag outside the named printers and pass it in. "Outside"
//     is literal twice over. drainChat is ITSELF in the list, so composing the
//     tag in drainChat and passing it to printChatLine trips the two-printers
//     FATAL. And the walk descends into function literals, so a closure inside
//     the printer is not outside it either — it trips the >1 branch. What
//     passes is a top-level helper func: a FuncDecl this list does not name.
//     Note this is the way out for the NEW tag. Moving the MESSAGE-ID tag into
//     a helper instead hits a fourth FATAL this paragraph is not about — no
//     named printer prints a bare local at all — whose answer is to re-point
//     notificationPrinters, not to widen anything.
//
// What is NOT a way through is loosening the match.
func notificationIDField(t *testing.T) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, ocagentListenSource, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", ocagentListenSource, err)
	}
	// Which NAMED printer emits the tag, and which local holds the value after "#"?
	var drain *ast.FuncDecl
	printed := ""
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !notificationPrinters[fn.Name.Name] {
			continue
		}
		// COLLECT, never overwrite: two `"#" + ident` in ONE printer is the same
		// ambiguity as two printers, and it used to resolve to whichever came
		// LAST in source order. A review demonstrated the pair — a real print of
		// the wrong field beside a never-printed decoy reading the right one —
		// flipping between red and green purely by swapping the two lines. A
		// guard whose verdict depends on line order is not a guard.
		var hits []string
		opaque := 0
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || be.Op != token.ADD {
				return true
			}
			lit, ok := be.X.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || lit.Value != `"#"` {
				return true
			}
			if id, ok := be.Y.(*ast.Ident); ok {
				hits = append(hits, id.Name)
				return true
			}
			// `"#" + <anything that is not a bare local>`: this derivation
			// cannot follow it to a payload key. Counting it is what keeps the
			// guard closed — a print of the wrong field written as an inline
			// call is INVISIBLE here, so without this an unrelated bare-ident
			// line elsewhere in the same printer becomes the thing we verify,
			// and the guard passes green while the notification names the wrong
			// message. That exact mutant survived the first attempt at this fix.
			opaque++
			return true
		})
		if opaque > 0 {
			t.Fatalf("%s builds `\"#\" + <expr>` in %s where <expr> is not a "+
				"bare local (%d such), so the payload key it names cannot be "+
				"derived — re-check it by hand", ocagentListenSource,
				fn.Name.Name, opaque)
		}
		if len(hits) > 1 {
			t.Fatalf("%s builds `\"#\" + <local>` %d times inside %s (%v) — "+
				"which one names the notification tag cannot be derived; "+
				"re-check it by hand", ocagentListenSource, len(hits),
				fn.Name.Name, hits)
		}
		if len(hits) == 0 {
			continue
		}
		here := hits[0]
		if printed != "" {
			t.Fatalf("%s prints `\"#\" + <local>` in more than one named printer "+
				"(%s and %s) — which one names the notification tag can no "+
				"longer be derived; re-check it by hand",
				ocagentListenSource, drain.Name.Name, fn.Name.Name)
		}
		drain, printed = fn, here
	}
	if printed == "" {
		t.Fatalf("none of the named notification printers in %s prints "+
			"`\"#\" + <local>` — either the tag changed shape or the printing "+
			"moved to a function not in notificationPrinters; re-check it by "+
			"hand and re-point that list", ocagentListenSource)
	}

	// Where did that local come from, and which payload key did it read?
	keys := []string{}
	ast.Inspect(drain.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		named := false
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == printed {
				named = true
			}
		}
		if !named {
			return true
		}
		for _, rhs := range as.Rhs {
			ast.Inspect(rhs, func(n ast.Node) bool {
				ix, ok := n.(*ast.IndexExpr)
				if !ok {
					return true
				}
				if lit, ok := ix.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					keys = append(keys, strings.Trim(lit.Value, `"`))
				}
				return true
			})
		}
		return true
	})
	if len(keys) != 1 {
		t.Fatalf("cannot tell which chat-payload key %s prints after the "+
			"'#': %q reads %v — re-check %s by hand",
			drain.Name.Name, printed, keys, ocagentListenSource)
	}
	return keys[0]
}
