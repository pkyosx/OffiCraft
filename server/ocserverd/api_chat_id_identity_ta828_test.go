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
// separate Go module (root CLAUDE.md「驗證、CI 與出貨」: the four modules cannot import each
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
	if err := json.Unmarshal(get("/api/chat?with=m-2"), &served); err != nil {
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
	if err := json.Unmarshal(get("/api/chat?ids="+fromSnapshot), &reread); err != nil {
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

// notificationIDField derives, from the ocagent source itself, the chat-payload
// field whose value is printed after the "#" on the notification line.
//
// It is DERIVED rather than written down here because a constant would be a
// second copy of the very thing this file is checking: someone renaming the
// field on the CLI side would edit their copy and leave ours agreeing with a
// server that no longer matches them.
//
// The derivation is structural: find `"#" + <ident>` inside drainChat, then find
// where that ident was assigned, and read the single string-literal map key in
// that assignment. Anything it cannot resolve is a FATAL — the shape of the
// line changed, and a human has to re-check this contract rather than have the
// test quietly widen.
func notificationIDField(t *testing.T) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, ocagentListenSource, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", ocagentListenSource, err)
	}
	var drain *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "drainChat" && fn.Recv == nil {
			drain = fn
			break
		}
	}
	if drain == nil {
		t.Fatalf("%s no longer declares drainChat — the notification line moved, "+
			"and this contract has to be re-derived by hand", ocagentListenSource)
	}

	// Which local holds the value printed after the "#"?
	printed := ""
	ast.Inspect(drain.Body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != token.ADD {
			return true
		}
		lit, ok := be.X.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || lit.Value != `"#"` {
			return true
		}
		if id, ok := be.Y.(*ast.Ident); ok {
			printed = id.Name
		}
		return true
	})
	if printed == "" {
		t.Fatalf("drainChat no longer prints `\"#\" + <local>` — the message-id "+
			"tag changed shape, so which payload field it names can no longer "+
			"be derived; re-check %s by hand", ocagentListenSource)
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
		t.Fatalf("cannot tell which chat-payload key drainChat prints after the "+
			"'#': %q reads %v — re-check %s by hand",
			printed, keys, ocagentListenSource)
	}
	return keys[0]
}
