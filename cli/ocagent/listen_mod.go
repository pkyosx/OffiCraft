package main

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

// --deliver-mod: stdout is the mod's protocol and nothing else, one JSON frame
// per line —
//
//	{"submit":"<payload>"}  one packed payload, to become one prompt
//	{"batch":"<token>"}     the ack gate's end-of-batch marker; the mod answers it
//	                        in OC_LISTEN_ACK_FILE once every submit before it settled
//
// in the order the listener printed them. Lines that are not forwarded go to
// stderr. The frame shapes are spelled again in cli/ocwarden/mod/hooks/register.ts.
type modFrame struct {
	Submit *string `json:"submit,omitempty"`
	Batch  *string `json:"batch,omitempty"`
}

type modWriter struct {
	*deliveryQueue
	out   io.Writer
	outMu sync.Mutex
}

func newModWriter(out, diag io.Writer) *modWriter {
	return &modWriter{deliveryQueue: newDeliveryQueue(diag), out: out}
}

func (w *modWriter) start() func() {
	return w.deliveryQueue.start(w.drain)
}

func (w *modWriter) drain() {
	w.deliveryQueue.drain(
		func(payload string) { w.emit(modFrame{Submit: &payload}) },
		func(token string) { w.emit(modFrame{Batch: &token}) },
	)
}

func (w *modWriter) emit(frame modFrame) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(frame); err != nil {
		w.note("listen: could not encode a frame for the mod (%v)\n", err)
		return
	}
	w.outMu.Lock()
	_, err := w.out.Write(buf.Bytes())
	w.outMu.Unlock()
	if err != nil {
		w.note("listen: the mod's stdout refused a frame (%v)\n", err)
	}
}
