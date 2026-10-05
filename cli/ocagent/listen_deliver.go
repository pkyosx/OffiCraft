package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// A claude member's listener does not print for the member to read: its events
// are delivered into the member's conversation, by one of two routes the warden
// picks per spawn (cli/ocwarden/spawn.go):
//   - --deliver-mod: the listener is the child of the member's own Claude Code
//     notification mod (cli/ocwarden/mod), which submits each payload as a
//     prompt and answers the ack gate;
//   - --deliver-tmux: the listener runs beside the member in its own tmux
//     session and pastes into the member's pane. The fallback for a Claude Code
//     without mods, or one that did not load the mod.

const listenCodexFlag = "deliver-codex"

const (
	// Owner ruling rc-62ede5d63772: anything bigger reaches the member as an id-only
	// notice. It counts the bytes actually delivered, header included, and has to
	// stay well under set-buffer's argv ceiling (~16.3 KB measured; the exact figure
	// moves with the socket and buffer name lengths).
	deliveryMaxBytes = 8 << 10

	oversizedNoticeHeaderRunes = 200
)

func listenSink(out, errOut io.Writer, env func(string) string, deliverTmux, deliverMod bool, run tmuxRun) (io.Writer, func(), bool) {
	if !deliverTmux && !deliverMod {
		return out, func() {}, true
	}
	if deliverTmux && deliverMod {
		fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-tmux and --deliver-mod are two routes "+
			"into the same member; pick one. Refusing to start.\n")
		return nil, func() {}, false
	}
	socket, session, ok := tmuxSessionFromEnv(env)
	if !ok {
		// 🔴 Refuse, do not degrade: without OC_SESSION makeSessionProbe returns
		// nil, so this listener could never self-exit and would hold the SSE —
		// i.e. keep a vanished member "online" — forever, saying nothing.
		flagName, w := "--deliver-tmux", out
		if deliverMod {
			flagName, w = "--deliver-mod", errOut
		}
		fmt.Fprint(w, agentLinePrefix+"listen: "+flagName+" needs OC_SESSION "+
			"(the session to deliver into, and the session this listener must die with); "+
			"refusing to start.\n")
		return nil, func() {}, false
	}
	if deliverMod {
		// Without the ack channel a submit the session refused would still be
		// marked read on the station.
		if env(listenAckEnv) != "1" || strings.TrimSpace(env(listenAckFileEnv)) == "" {
			fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-mod needs "+listenAckEnv+"=1 and "+
				listenAckFileEnv+" (the file the mod answers each batch in); refusing to start.\n")
			return nil, func() {}, false
		}
		w := newModWriter(out, errOut)
		return w, w.start(), true
	}
	w := newPaneWriter(out, socket, session, run, nil)
	return w, w.start(), true
}

// Claude delivery must pass through listenSink; raw stdout never reaches its conversation.
func cmdListen(argv []string, cfg Config, env func(string) string, out, errOut io.Writer,
	start func(Config, func(string) string, bool, io.Writer) int, run tmuxRun) int {
	fs := flag.NewFlagSet("ocagent listen", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "do a single connect then return (test/diagnostic hook)")
	deliverTmux := fs.Bool("deliver-tmux", false,
		"run beside the member: deliver each event into OC_SESSION's pane instead of expecting it to read this stdout")
	deliverMod := fs.Bool("deliver-mod", false,
		"run under the member's notification mod: print one JSON frame per line on stdout "+
			"(submit payloads and batch markers), diagnostics on stderr, acks read from OC_LISTEN_ACK_FILE")
	deliverCodex := fs.Bool(listenCodexFlag, false, "print each complete notice as one JSON frame for the Codex sidecar")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *deliverCodex {
		if *deliverTmux || *deliverMod || env(listenAckEnv) != "1" {
			fmt.Fprint(errOut, agentLinePrefix+"listen: --deliver-codex requires OC_LISTEN_ACK=1 and cannot be combined with other delivery routes\n")
			return 2
		}
		return start(cfg, env, *once, &codexFrameWriter{out: out})
	}
	sink, stop, ok := listenSink(out, errOut, env, *deliverTmux, *deliverMod, run)
	if !ok {
		return 2
	}
	defer stop()
	return start(cfg, env, *once, sink)
}

// Packs whole events (a column-0 line plus its indented continuation lines) into
// payloads of at most deliveryMaxBytes; an event that alone exceeds it is replaced
// by its id-only notice, so no payload is ever split between lines (owner ruling).
func packDeliveries(lines []string) []string {
	var payloads []string
	var current string
	for _, event := range splitEvents(lines) {
		if len(event) > deliveryMaxBytes {
			event = oversizedEventNotice(event)
		}
		if current != "" && len(current)+1+len(event) > deliveryMaxBytes {
			payloads = append(payloads, current)
			current = ""
		}
		if current == "" {
			current = event
		} else {
			current += "\n" + event
		}
	}
	if current != "" {
		payloads = append(payloads, current)
	}
	return payloads
}

func splitEvents(lines []string) []string {
	var events []string
	for _, line := range lines {
		isContinuation := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if isContinuation && len(events) > 0 {
			events[len(events)-1] += "\n" + line
			continue
		}
		events = append(events, line)
	}
	return events
}

func oversizedEventNotice(event string) string {
	header, _, _ := strings.Cut(event, "\n")
	return previewLine(header, oversizedNoticeHeaderRunes) + fmt.Sprintf(
		" [這則通知約 %d 行／%d 字，超過送進畫面的上限 %d KiB，正文沒有送進來 — 請用 %s 讀全文]",
		strings.Count(event, "\n")+1, utf8.RuneCountInString(event), deliveryMaxBytes>>10, fullReadToolFor(header))
}

func fullReadToolFor(header string) string {
	switch {
	case strings.HasPrefix(header, agentLinePrefix+"reply-card "):
		return replyCardFullReadTool
	case strings.HasPrefix(header, agentLinePrefix+"task "):
		return "get_task"
	default:
		return chatFullReadTool
	}
}

func takeLine(buf *bytes.Buffer) (string, bool) {
	all := buf.Bytes()
	i := bytes.IndexByte(all, '\n')
	if i < 0 {
		return "", false
	}
	line := string(all[:i])
	rest := append([]byte(nil), all[i+1:]...)
	buf.Reset()
	buf.Write(rest)
	return line, true
}

type bootConnectFilter struct {
	firstConnectSettled bool
}

// The connect that opens a BOOT is swallowed: the boot turn is already running
// or queued (pasted by the warden, or submitted by the mod before it starts the
// listener) and reads the station itself. Later connects mean "the stream is
// back" and are forwarded.
//
// 🔴 "Opens a boot" ≠ "first connect printed": if the first dial fails, the
// forwarded disconnect notice promised the member that the next
// transport line is the reconnect or a give-up — so forwarding a disconnect or
// give-up spends the boot swallow. The codex sidecar is no precedent for plain
// swallowing: it replaces that line with a post-boot wake (codex_session.go).
func (f *bootConnectFilter) shouldForward(line string) bool {
	if !forwardToMember(line) {
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeConnected) && !f.firstConnectSettled {
		f.firstConnectSettled = true
		return false
	}
	if strings.HasPrefix(line, agentLinePrefix+noticeDisconnected) ||
		strings.HasPrefix(line, agentLinePrefix+noticeGivingUp) {
		f.firstConnectSettled = true
	}
	return true
}

// Owner's disconnect-notice ruling: event lines reach the member, transport
// chatter does not — except the three notices it names.
//
// 🔴 Match at column 0, never after a trim: chat bodies arrive INDENTED, and a
// message quoting a transport line would otherwise be swallowed as one of ours —
// silently and for good (the chat is already receipted as read).
func forwardToMember(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	rest, isOurs := strings.CutPrefix(line, agentLinePrefix)
	if !isOurs || !strings.HasPrefix(rest, "listen:") {
		return true
	}
	for _, notice := range []string{noticeDisconnected, noticeConnected, noticeGivingUp} {
		if strings.HasPrefix(rest, notice) {
			return true
		}
	}
	return false
}
