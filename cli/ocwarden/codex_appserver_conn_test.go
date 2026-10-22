package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeAppServer accepts one connection on a short unix socket path and hands
// it to serve; answer writes the upgrade response.
func fakeAppServer(t *testing.T, answer func(key string) string, serve func(conn net.Conn, r *bufio.Reader)) string {
	t.Helper()
	path := filepath.Join(shortTempDir(t), "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		req, err := http.ReadRequest(r)
		if err != nil {
			return
		}
		_, _ = io.WriteString(conn, answer(req.Header.Get("Sec-WebSocket-Key")))
		if serve != nil {
			serve(conn, r)
		}
	}()
	return path
}

func upgradeAnswer(key string) string {
	return "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + websocketAccept(key) + "\r\n\r\n"
}

// readRawClientFrame keeps the mask bit visible, which readWebSocketFrame hides.
func readRawClientFrame(t *testing.T, r io.Reader) (opcode byte, masked bool, payload []byte) {
	t.Helper()
	head := make([]byte, 2)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Errorf("read client frame: %v", err)
		return 0, false, nil
	}
	n := int(head[1] & 0x7F)
	masked = head[1]&0x80 != 0
	var key [4]byte
	if masked {
		_, _ = io.ReadFull(r, key[:])
	}
	payload = make([]byte, n)
	_, _ = io.ReadFull(r, payload)
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return head[0] & 0x0F, masked, payload
}

func nextMessage(t *testing.T, ch <-chan appServerMessage) (appServerMessage, bool) {
	t.Helper()
	select {
	case msg, ok := <-ch:
		return msg, ok
	case <-time.After(5 * time.Second):
		t.Fatal("no message from the fake App Server within 5s")
		return nil, false
	}
}

func TestWebsocketAccept(t *testing.T) {
	if got := websocketAccept("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("websocketAccept(RFC 6455 sample) = %q, want %q", got, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=")
	}
}

func TestDialCodexAppServer(t *testing.T) {
	t.Run("an upgraded connection carries one masked message out and one message back", func(t *testing.T) {
		type clientFrame struct {
			opcode  byte
			masked  bool
			payload string
		}
		got := make(chan clientFrame, 1)
		path := fakeAppServer(t, upgradeAnswer, func(conn net.Conn, r *bufio.Reader) {
			op, masked, payload := readRawClientFrame(t, r)
			got <- clientFrame{op, masked, string(payload)}
			_, _ = conn.Write(encodeWebSocketFrame(wsOpText, []byte(`{"id":1,"result":{"ok":true}}`), false))
			time.Sleep(200 * time.Millisecond)
		})

		c, err := dialCodexAppServer(path, 2*time.Second)
		if err != nil {
			t.Fatalf("dialCodexAppServer: %v", err)
		}
		defer c.Close()
		n, err := c.Write([]byte(`{"id":1,"method":"initialize"}`))
		if err != nil || n != 30 {
			t.Fatalf("Write = %d, %v; want 30, nil", n, err)
		}

		if f := <-got; f != (clientFrame{wsOpText, true, `{"id":1,"method":"initialize"}`}) {
			t.Errorf("the server received %+v, want a masked text frame carrying the message", f)
		}
		msg, ok := nextMessage(t, c.messages())
		if want := (appServerMessage{"id": float64(1), "result": map[string]any{"ok": true}}); !ok || !reflect.DeepEqual(msg, want) {
			t.Errorf("messages() yielded %v (open %v), want %v", msg, ok, want)
		}
	})

	t.Run("a ping is answered with a pong carrying the same payload", func(t *testing.T) {
		pong := make(chan webSocketFrame, 1)
		path := fakeAppServer(t, upgradeAnswer, func(conn net.Conn, r *bufio.Reader) {
			_, _ = conn.Write(encodeWebSocketFrame(wsOpPing, []byte("hb-1"), false))
			f, err := readWebSocketFrame(r, 1024)
			if err != nil {
				t.Errorf("read pong: %v", err)
			}
			pong <- f
		})
		c, err := dialCodexAppServer(path, 2*time.Second)
		if err != nil {
			t.Fatalf("dialCodexAppServer: %v", err)
		}
		defer c.Close()
		c.messages()

		select {
		case f := <-pong:
			if f.opcode != wsOpPong || string(f.payload) != "hb-1" || !f.fin {
				t.Errorf("the server received %+v, want a final pong carrying %q", f, "hb-1")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no pong within 5s")
		}
	})

	t.Run("a message split into a text frame and a continuation arrives whole", func(t *testing.T) {
		path := fakeAppServer(t, upgradeAnswer, func(conn net.Conn, _ *bufio.Reader) {
			first, rest := `{"id":2,`, `"result":{}}`
			_, _ = conn.Write(append([]byte{wsOpText, byte(len(first))}, first...))
			_, _ = conn.Write(append([]byte{0x80 | wsOpContinuation, byte(len(rest))}, rest...))
			time.Sleep(200 * time.Millisecond)
		})
		c, err := dialCodexAppServer(path, 2*time.Second)
		if err != nil {
			t.Fatalf("dialCodexAppServer: %v", err)
		}
		defer c.Close()

		msg, ok := nextMessage(t, c.messages())
		if want := (appServerMessage{"id": float64(2), "result": map[string]any{}}); !ok || !reflect.DeepEqual(msg, want) {
			t.Errorf("messages() yielded %v (open %v), want %v", msg, ok, want)
		}
	})

	t.Run("a close frame ends the message stream", func(t *testing.T) {
		path := fakeAppServer(t, upgradeAnswer, func(conn net.Conn, _ *bufio.Reader) {
			_, _ = conn.Write(encodeWebSocketFrame(wsOpClose, nil, false))
			time.Sleep(200 * time.Millisecond)
		})
		c, err := dialCodexAppServer(path, 2*time.Second)
		if err != nil {
			t.Fatalf("dialCodexAppServer: %v", err)
		}
		defer c.Close()

		if msg, ok := nextMessage(t, c.messages()); ok {
			t.Errorf("messages() yielded %v after a close frame, want the channel closed", msg)
		}
	})

	t.Run("an upgrade answering the wrong accept value is refused", func(t *testing.T) {
		path := fakeAppServer(t, func(string) string {
			return "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: bogus\r\n\r\n"
		}, nil)

		_, err := dialCodexAppServer(path, 2*time.Second)

		if err == nil || !strings.HasPrefix(err.Error(), `websocket upgrade accept "bogus", want "`) {
			t.Errorf("dialCodexAppServer error = %v, want the accept mismatch", err)
		}
	})

	t.Run("an answer other than 101 is refused", func(t *testing.T) {
		path := fakeAppServer(t, func(string) string {
			return "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"
		}, nil)

		_, err := dialCodexAppServer(path, 2*time.Second)

		if err == nil || err.Error() != "websocket upgrade answered 404 Not Found" {
			t.Errorf("dialCodexAppServer error = %v, want %q", err, "websocket upgrade answered 404 Not Found")
		}
	})
}

func TestRemoveCodexAppServerSocket(t *testing.T) {
	exists := func(path string) bool {
		_, err := os.Lstat(path)
		return err == nil
	}

	t.Run("a symlink to a socket is removed together with the socket", func(t *testing.T) {
		dir := shortTempDir(t)
		real, link := filepath.Join(dir, "real.sock"), filepath.Join(dir, ".codex-app.sock")
		leaveUnixSocket(t, real)
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}

		removeCodexAppServerSocket(link)

		if exists(link) || exists(real) {
			t.Errorf("after removal link exists=%v socket exists=%v, want both gone", exists(link), exists(real))
		}
	})

	t.Run("a socket at the path itself is removed", func(t *testing.T) {
		path := filepath.Join(shortTempDir(t), ".codex-app.sock")
		leaveUnixSocket(t, path)

		removeCodexAppServerSocket(path)

		if exists(path) {
			t.Error("the socket still exists")
		}
	})

	t.Run("a regular file at the path is left alone", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".codex-app.sock")
		if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}

		removeCodexAppServerSocket(path)

		if raw, err := os.ReadFile(path); err != nil || string(raw) != "keep" {
			t.Errorf("the regular file reads %q, %v; want it untouched", raw, err)
		}
	})

	t.Run("a symlink to a regular file loses the link but keeps the file", func(t *testing.T) {
		dir := t.TempDir()
		target, link := filepath.Join(dir, "notes"), filepath.Join(dir, ".codex-app.sock")
		if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}

		removeCodexAppServerSocket(link)

		if exists(link) || !exists(target) {
			t.Errorf("link exists=%v target exists=%v, want the link gone and the target kept", exists(link), exists(target))
		}
	})

	t.Run("a missing path changes nothing", func(t *testing.T) {
		dir := t.TempDir()

		removeCodexAppServerSocket(filepath.Join(dir, ".codex-app.sock"))

		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("dir holds %v, want it empty", entries)
		}
	})
}

// leaveUnixSocket leaves a socket file with nobody listening, as a SIGKILLed
// App Server does.
func leaveUnixSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
}
