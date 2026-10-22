package main

// The App Server's unix-socket listener speaks WebSocket (an HTTP Upgrade, then
// RFC 6455 frames), not the JSON lines of its stdio mode. cli/ modules stay
// stdlib-only, so this is the minimum client the sidecar needs: one text
// message per JSON-RPC message, masked as every client frame must be.

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	codexAppServerSocketName = ".codex-app.sock"
	// Codex advertises 16 MiB per unfragmented message and fragments above it;
	// the cap bounds one reassembled message, like the stdio reader's line cap.
	codexAppMaxMessageBytes = 64 << 20
	websocketAcceptGUID     = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

const (
	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA
)

func codexAppServerSocketPath(workdir string) string {
	return filepath.Join(workdir, codexAppServerSocketName)
}

// Codex keeps the real socket under the system temp dir and leaves a symlink at
// the path it was given; a SIGKILLed App Server leaves both behind.
func removeCodexAppServerSocket(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(path); err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			if t, err := os.Lstat(target); err == nil && t.Mode()&os.ModeSocket != 0 {
				_ = os.Remove(target)
			}
		}
	} else if info.Mode()&os.ModeSocket == 0 {
		return
	}
	_ = os.Remove(path)
}

type codexAppConn struct {
	conn    net.Conn
	r       *bufio.Reader
	writeMu sync.Mutex
}

func dialCodexAppServer(socketPath string, timeout time.Duration) (*codexAppConn, error) {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return nil, err
	}
	c := &codexAppConn{conn: conn, r: bufio.NewReader(conn)}
	if err := c.handshake(timeout); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *codexAppConn) handshake(timeout time.Duration) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	key := base64.StdEncoding.EncodeToString(nonce)
	req, err := http.NewRequest(http.MethodGet, "http://localhost/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", key)
	req.Header.Set("Sec-WebSocket-Version", "13")
	_ = c.conn.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()
	if err := req.Write(c.conn); err != nil {
		return err
	}
	resp, err := http.ReadResponse(c.r, req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("websocket upgrade answered %s", resp.Status)
	}
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), websocketAccept(key); got != want {
		return fmt.Errorf("websocket upgrade accept %q, want %q", got, want)
	}
	return nil
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + websocketAcceptGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Write sends p as ONE text message: json.Encoder hands over a whole encoded
// value per call, which is what keeps one JSON-RPC message per frame.
func (c *codexAppConn) Write(p []byte) (int, error) {
	if err := c.writeFrame(wsOpText, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *codexAppConn) Close() error {
	_ = c.writeFrame(wsOpClose, nil)
	return c.conn.Close()
}

func (c *codexAppConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(encodeWebSocketFrame(opcode, payload, true))
	return err
}

func encodeWebSocketFrame(opcode byte, payload []byte, masked bool) []byte {
	frame := []byte{0x80 | opcode}
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, maskBit|byte(n))
	case n <= 0xFFFF:
		frame = append(frame, maskBit|126)
		frame = binary.BigEndian.AppendUint16(frame, uint16(n))
	default:
		frame = append(frame, maskBit|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}
	if !masked {
		return append(frame, payload...)
	}
	var key [4]byte
	_, _ = rand.Read(key[:])
	frame = append(frame, key[:]...)
	start := len(frame)
	frame = append(frame, payload...)
	for i := range payload {
		frame[start+i] ^= key[i%4]
	}
	return frame
}

type webSocketFrame struct {
	fin     bool
	opcode  byte
	payload []byte
}

func readWebSocketFrame(r io.Reader, limit int) (webSocketFrame, error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return webSocketFrame{}, err
	}
	f := webSocketFrame{fin: head[0]&0x80 != 0, opcode: head[0] & 0x0F}
	masked := head[1]&0x80 != 0
	n := uint64(head[1] & 0x7F)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return f, err
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return f, err
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	if n > uint64(limit) {
		return f, fmt.Errorf("websocket frame of %d bytes exceeds %d", n, limit)
	}
	var key [4]byte
	if masked {
		if _, err := io.ReadFull(r, key[:]); err != nil {
			return f, err
		}
	}
	f.payload = make([]byte, n)
	if _, err := io.ReadFull(r, f.payload); err != nil {
		return f, err
	}
	if masked {
		for i := range f.payload {
			f.payload[i] ^= key[i%4]
		}
	}
	return f, nil
}

// readMessage returns the next data message, answering pings on the way. A
// close frame or a broken stream ends the conversation with an error.
func (c *codexAppConn) readMessage() ([]byte, error) {
	var message []byte
	for {
		f, err := readWebSocketFrame(c.r, codexAppMaxMessageBytes)
		if err != nil {
			return nil, err
		}
		switch f.opcode {
		case wsOpPing:
			if err := c.writeFrame(wsOpPong, f.payload); err != nil {
				return nil, err
			}
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			return nil, io.EOF
		case wsOpText, wsOpBinary, wsOpContinuation:
		default:
			return nil, fmt.Errorf("websocket opcode %#x is not defined", f.opcode)
		}
		if len(message)+len(f.payload) > codexAppMaxMessageBytes {
			return nil, errors.New("websocket message exceeds the reassembly cap")
		}
		message = append(message, f.payload...)
		if f.fin {
			return message, nil
		}
	}
}

// messages ends (closes the channel) when the App Server goes away, exactly as
// the stdio reader did on EOF: every caller already treats that as "exited".
func (c *codexAppConn) messages() <-chan appServerMessage {
	out := make(chan appServerMessage, 64)
	go func() {
		defer close(out)
		for {
			raw, err := c.readMessage()
			if err != nil {
				return
			}
			var msg appServerMessage
			if json.Unmarshal(raw, &msg) == nil {
				out <- msg
			}
		}
	}()
	return out
}
