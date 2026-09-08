package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// markReadServer serves one page of unread chat and records every read receipt
// exactly as it came off the wire.
type markReadServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newMarkReadServer(t *testing.T, list string, receiptStatus int) *markReadServer {
	t.Helper()
	return newChatServer(t, list, 200, receiptStatus)
}

// newBrokenChatServer refuses every chat refetch while still recording anything
// that reaches the read-receipt route.
func newBrokenChatServer(t *testing.T) *markReadServer {
	t.Helper()
	return newChatServer(t, "[]", 500, 200)
}

func newChatServer(t *testing.T, list string, chatStatus, receiptStatus int) *markReadServer {
	t.Helper()
	m := &markReadServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == markReadPath {
			raw, _ := io.ReadAll(r.Body)
			m.mu.Lock()
			m.bodies = append(m.bodies, string(raw))
			m.mu.Unlock()
			var receipt struct {
				Peer string `json:"peer"`
			}
			_ = json.Unmarshal(raw, &receipt)
			w.WriteHeader(receiptStatus)
			_, _ = w.Write([]byte(`{"peer_id":"` + receipt.Peer +
				`","last_read_ts":0,"advanced":true}`))
			return
		}
		w.WriteHeader(chatStatus)
		if chatStatus == 200 {
			_, _ = w.Write([]byte(`{"messages":` + list + `}`))
		}
	}))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *markReadServer) receipts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

func chatMessage(id, from string, ts float64) string {
	return fmt.Sprintf(`{"id":%q,"from":%q,"to":"kyle","body":"body-%s","ts":%g}`, id, from, id, ts)
}

func markReadCfg(base, home string) Config {
	return Config{Base: base, Token: "t", ID: "kyle", Home: home}
}

// TestDrainChatFilesReadReceipts pins the read receipts drainChat puts on the
// wire: one POST per SENDER, carrying that sender's own high-water mark, and a
// body the frozen MarkChatReadDTO accepts — the server decodes it with
// DisallowUnknownFields, so one undeclared key leaves the batch unread forever
// while at most one warning is ever printed.
func TestDrainChatFilesReadReceipts(t *testing.T) {
	declared := frozenIngestProperties(t, "MarkChatReadDTO")
	now := float64(time.Now().Unix())
	list := "[" + strings.Join([]string{
		chatMessage("a1", "alice", now-90),
		chatMessage("b1", "bob", now-80),
		chatMessage("a2", "alice", now-70),
	}, ",") + "]"

	walked := map[string]int{}

	t.Run("each sender is marked to the newest line of theirs that printed", func(t *testing.T) {
		srv := newMarkReadServer(t, list, 200)
		var out bytes.Buffer

		printed := drainChat(srv.Client(), markReadCfg(srv.URL, t.TempDir()), &out,
			&drainWarner{}, nil)

		if printed != 3 {
			t.Errorf("drainChat reported %d unread lines, want 3", printed)
		}
		wantOut := "[ocagent] chat from alice (#a1, 1m ago): body-a1\n" +
			"[ocagent] chat from bob (#b1, 1m ago): body-b1\n" +
			"[ocagent] chat from alice (#a2, 1m ago): body-a2\n"
		if out.String() != wantOut {
			t.Errorf("printed\n%q\nwant\n%q", out.String(), wantOut)
		}

		bodies := srv.receipts()
		wantBodies := []string{
			fmt.Sprintf(`{"last_read_ts":%.0f,"peer":"alice"}`, now-70),
			fmt.Sprintf(`{"last_read_ts":%.0f,"peer":"bob"}`, now-80),
		}
		if !reflect.DeepEqual(bodies, wantBodies) {
			t.Fatalf("read receipts =\n  %q\nwant\n  %q", bodies, wantBodies)
		}
		for _, body := range bodies {
			walked[markReadPath]++
			if bad := schemaViolations(body, declared); len(bad) > 0 {
				t.Errorf("mark-read body has keys the frozen schema refuses %v — the receipt "+
					"would be rejected and the batch would reprint forever; body=%s", bad, body)
			}
		}
	})

	t.Run("a refused receipt is announced once for the whole process", func(t *testing.T) {
		srv := newMarkReadServer(t, list, 503)
		var out bytes.Buffer

		printed := drainChat(srv.Client(), markReadCfg(srv.URL, t.TempDir()), &out,
			&drainWarner{}, nil)

		if printed != 3 {
			t.Errorf("drainChat reported %d unread lines, want 3 — a refused receipt must "+
				"not swallow the lines that already printed", printed)
		}
		if got := len(srv.receipts()); got != 2 {
			t.Errorf("filed %d receipts, want 2 — both senders are still attempted", got)
		}
		wantOut := "[ocagent] chat from alice (#a1, 1m ago): body-a1\n" +
			"[ocagent] chat from bob (#b1, 1m ago): body-b1\n" +
			"[ocagent] chat from alice (#a2, 1m ago): body-a2\n" +
			"[ocagent] mark-read 沒送成功（peer=alice，HTTP 503）— 訊息已經印出來了，" +
			"但是回條沒送成功，server 那邊就還算未讀：這一批下一次補印會再印一次，" +
			"而且會一直重印到回條送成功為止，在那之前對方的已讀勾也不會亮。" +
			"這個行程只會講這一次 —— 之後再看到同一批訊息重複出現，原因就是這一行。\n"
		if out.String() != wantOut {
			t.Errorf("printed\n%q\nwant\n%q", out.String(), wantOut)
		}
	})

	t.Run("an empty inbox prints nothing and files no receipt", func(t *testing.T) {
		srv := newMarkReadServer(t, "[]", 200)
		var out bytes.Buffer

		printed := drainChat(srv.Client(), markReadCfg(srv.URL, t.TempDir()), &out,
			&drainWarner{}, nil)

		if printed != 0 {
			t.Errorf("drainChat reported %d unread lines, want 0", printed)
		}
		if out.String() != "" {
			t.Errorf("printed %q, want nothing", out.String())
		}
		if bodies := srv.receipts(); !reflect.DeepEqual(bodies, []string(nil)) {
			t.Errorf("read receipts = %q, want none — the ✓ would then mean "+
				"\"a listener connected\", not \"someone read it\"", bodies)
		}
	})

	t.Run("a chat refetch that fails says so and files no receipt", func(t *testing.T) {
		srv := newBrokenChatServer(t)
		var out bytes.Buffer

		printed := drainChat(srv.Client(), markReadCfg(srv.URL, t.TempDir()), &out,
			&drainWarner{}, nil)

		if printed != 0 {
			t.Errorf("drainChat reported %d unread lines, want 0", printed)
		}
		wantOut := "[ocagent] chat: 補印一頁都沒撈到（HTTP 500）—— 這不是「沒有新訊息」，" +
			"是這次沒問到。未讀原封不動，下一次補印會再試；等不及就用 get_chat 自己撈。\n"
		if out.String() != wantOut {
			t.Errorf("printed\n%q\nwant\n%q", out.String(), wantOut)
		}
		if bodies := srv.receipts(); !reflect.DeepEqual(bodies, []string(nil)) {
			t.Errorf("read receipts = %q, want none — a window nobody fetched would "+
				"otherwise be swept read unseen", bodies)
		}
	})

	t.Run("a message the wire sent without a ts prints but carries no watermark", func(t *testing.T) {
		mixed := `[{"id":"m1","from":"boss","to":"kyle","body":"body-m1"},` +
			chatMessage("a1", "alice", now-40) + `]`
		srv := newMarkReadServer(t, mixed, 200)
		var out bytes.Buffer

		printed := drainChat(srv.Client(), markReadCfg(srv.URL, t.TempDir()), &out,
			&drainWarner{}, nil)

		if printed != 2 {
			t.Errorf("drainChat reported %d unread lines, want 2", printed)
		}
		wantOut := "[ocagent] chat from boss (#m1): body-m1\n" +
			"[ocagent] chat from alice (#a1, 40s ago): body-a1\n"
		if out.String() != wantOut {
			t.Errorf("printed\n%q\nwant\n%q", out.String(), wantOut)
		}
		bodies := srv.receipts()
		wantBodies := []string{
			fmt.Sprintf(`{"last_read_ts":%.0f,"peer":"alice"}`, now-40),
		}
		if !reflect.DeepEqual(bodies, wantBodies) {
			t.Errorf("read receipts =\n  %q\nwant\n  %q — boss has no ts to report, "+
				"and alice's line proves the drain still files what it can", bodies, wantBodies)
		}
	})

	t.Run("a note this member sent to itself is not read back to it and is still receipted", func(t *testing.T) {
		mine := "[" + strings.Join([]string{
			chatMessage("self-1", "kyle", now-300),
			chatMessage("m1", "boss", now-200),
		}, ",") + "]"
		srv := newMarkReadServer(t, mine, 200)
		var out bytes.Buffer

		printed := drainChat(srv.Client(), markReadCfg(srv.URL, t.TempDir()), &out,
			&drainWarner{}, nil)

		if printed != 1 {
			t.Errorf("drainChat reported %d unread lines, want 1 — a self-sent row is "+
				"not unread mail", printed)
		}
		wantOut := "[ocagent] chat from boss (#m1, 3m ago): body-m1\n"
		if out.String() != wantOut {
			t.Errorf("printed\n%q\nwant\n%q", out.String(), wantOut)
		}
		bodies := srv.receipts()
		wantBodies := []string{
			fmt.Sprintf(`{"last_read_ts":%.0f,"peer":"boss"}`, now-200),
			fmt.Sprintf(`{"last_read_ts":%.0f,"peer":"kyle"}`, now-300),
		}
		if !reflect.DeepEqual(bodies, wantBodies) {
			t.Errorf("read receipts =\n  %q\nwant\n  %q — the unprinted self-sent row "+
				"is still marked, or it comes back on every walk from here on",
				bodies, wantBodies)
		}
	})

	want := manifestUplinkPaths(t, "cli/ocagent/listen_markread_test.go")
	for route, rows := range want {
		if rows != 1 {
			t.Fatalf("cli/uplinks.json commits %d uplinks to %s through this wire test; the "+
				"join below compares route SETS and cannot tell them apart", rows, route)
		}
	}
	seen := map[string]int{}
	for route := range walked {
		seen[route] = 1
	}
	if !maps.Equal(seen, want) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the listener posted to %v",
			want, walked)
	}
}
