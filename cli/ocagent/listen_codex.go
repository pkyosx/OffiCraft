package main

import (
	"bytes"
	"encoding/json"
	"io"
)

type codexFrameWriter struct{ out io.Writer }

func (w *codexFrameWriter) Write(p []byte) (int, error) {
	frame := struct {
		Text string `json:"text"`
	}{Text: string(p)}
	if err := json.NewEncoder(w.out).Encode(frame); err != nil {
		return 0, err
	}
	return len(p), nil
}

func writeListenerNotice(out io.Writer, write func(io.Writer)) {
	stamped, ok := out.(*stampWriter)
	if !ok {
		write(out)
		return
	}
	framed, ok := stamped.inner.(*codexFrameWriter)
	if !ok {
		write(out)
		return
	}
	var buf bytes.Buffer
	notice := *stamped
	notice.inner = &buf
	write(&notice)
	_, _ = framed.Write(buf.Bytes())
}
