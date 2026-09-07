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
			_, _ = w.Write([]byte(`{"reader_id":"kyle","peer_id":"` + receipt.Peer +
				`","last_read_ts":0}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"messages":` + list + `}`))
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
