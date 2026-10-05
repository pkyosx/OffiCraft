package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestCodexFrameWriter(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"a single line is one text frame", "[ocagent] chat from owner: hello\n", `{"text":"[ocagent] chat from owner: hello\n"}` + "\n"},
		{"multiline content keeps blank lines indentation quotes and control-like text in one frame", "[ocagent] chat from owner: \"hello\"\n\n    indented\n\t[ocagent] listen: batch 99\n{\"text\":\"nested\"}\n", `{"text":"[ocagent] chat from owner: \"hello\"\n\n    indented\n\t[ocagent] listen: batch 99\n{\"text\":\"nested\"}\n"}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			w := &codexFrameWriter{out: &out}
			n, err := w.Write([]byte(tc.text))
			if n != len(tc.text) || err != nil {
				t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(tc.text))
			}
			if got := out.String(); got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("an output failure reports no delivered bytes", func(t *testing.T) {
		failure := errors.New("closed output")
		w := &codexFrameWriter{out: failingCodexOutput{err: failure}}
		n, err := w.Write([]byte("notice\n"))
		if n != 0 || !errors.Is(err, failure) {
			t.Errorf("Write = (%d, %v), want (0, %v)", n, err, failure)
		}
	})
}

type failingCodexOutput struct{ err error }

func (w failingCodexOutput) Write([]byte) (int, error) { return 0, w.err }

func TestWriteListenerNotice(t *testing.T) {
	cases := []struct {
		name    string
		framed  bool
		stamped bool
		want    string
	}{
		{"Codex combines writes into one stamped text frame", true, true, `{"text":"first [ts=12.345]\n\n  \"second\" [ts=12.345]\n"}` + "\n"},
		{"Claude keeps the stamped bytes of every write", false, true, "first [ts=12.345]\n\n  \"second\" [ts=12.345]\n"},
		{"plain output keeps the original bytes", false, false, "first\n\n  \"second\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			var sink io.Writer = &out
			if tc.framed {
				sink = &codexFrameWriter{out: sink}
			}
			if tc.stamped {
				sink = &stampWriter{inner: sink, stamp: func() string { return "[ts=12.345]" }}
			}
			writeListenerNotice(sink, func(w io.Writer) {
				io.WriteString(w, "first\n\n")
				io.WriteString(w, "  \"second\"\n")
			})
			if got := out.String(); got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}
