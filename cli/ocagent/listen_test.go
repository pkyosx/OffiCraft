// Skeleton generated from cli/ocagent/listen.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestScanSSE(t *testing.T) {
	t.Skip("TODO: scanSSE reads Server-Sent-Events from r, driving sink per the parts of the SSE line protocol officraft emits: `\\n`-separated lines (CRLF tolerated); a BLANK line is the event boundary → accumulated data dispatched; a `:` line is a comment/keepalive; `field: value` strips ONE leading space after the colon; `data:` lines join with \\n; `id:` feeds the cursor; every other field (event/retry/…) is ignored.")
}

func TestNextBackoff(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- backoff — Python next_backoff: exponential, capped, with full jitter.")
}

func TestCursorPath(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- per-agent SSE cursor (Last-Event-ID persistence — pure replay optimisation).")
}

func TestReadCursor(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestWriteCursor(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestResolveTmuxBin(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- session-liveness probe (the listener's self-exit lifecycle tie).")
}

func TestIsExecutableFileListen(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestMakeSessionProbe(t *testing.T) {
	t.Skip("TODO: makeSessionProbe builds the \"is my tmux session still alive?\" probe from the launch env, or nil when probing is DISABLED (no OC_SESSION — a headless run has no session to mirror).")
}

func TestShouldDispatch(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- SSE wake gates + wake handler (WORK signals; chat is refetch, not payload).")
}

func TestShouldWindDown(t *testing.T) {
	t.Skip("TODO: shouldWindDown mirrors should_wind_down: True ONLY for a `member` delta whose scoped key (<owner>::<id>) names THIS agent (suffix == my id).")
}

func TestFrameTrigger(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- frame attribution + echo suppression (spec/sse.md §2.3, T-f39c 方案 A).")
}

func TestIsSelfEcho(t *testing.T) {
	t.Skip("TODO: isSelfEcho is the ONE echo-suppression predicate: true iff the frame was triggered by THIS agent itself.")
}

func TestByTrigger(t *testing.T) {
	t.Skip("TODO: byTrigger renders the \" · by <actor>\" attribution suffix every event line carries (an agent reading \"by 自己\" on its own stream = the suppression is broken); blank attribution renders nothing rather than lying.")
}

func TestPreviewLine(t *testing.T) {
	t.Skip("TODO: previewLine collapses all whitespace (newlines included — event lines are ONE line) and truncates to max runes with an ellipsis.")
}

func TestRenderMessageBody(t *testing.T) {
	t.Skip("TODO: renderMessageBody prepares a MESSAGE event's body (chat body, reply-card summary/answer text) for the transcript.")
}

func TestHandleEvent(t *testing.T) {
	t.Skip("TODO: handleEvent is the wake handler for a WORK delta: log the wake.")
}

func TestHandleDirectedBand(t *testing.T) {
	t.Skip("TODO: handleDirectedBand surfaces one directed band frame as a single human- readable line on out — the spawned session's Monitor carries out into the agent's transcript, so this print IS the agent \"receiving\" the signal (before this handler existed the frames arrived and were silently dropped).")
}

func TestHandleTaskEvent(t *testing.T) {
	t.Skip("TODO: handleTaskEvent turns ONE task delta into ONE readable line: which task (task_no + title), what moved (status flip / step progress vs the last snapshot), and who moved it (the frame trigger).")
}

func TestIntField(t *testing.T) {
	t.Skip("TODO: intField reads a JSON-decoded number as int (0 on anything else).")
}

func TestFetchChat(t *testing.T) {
	t.Skip("TODO: fetchChat walks the caller's OWN UNREAD from GET /api/chat?recipient=<selfID>&unread=true&limit=… , following `next_cursor` until the server stops issuing one.")
}

func TestFmtAgo(t *testing.T) {
	t.Skip("TODO: fmtAgo renders an age in seconds as the terse single-unit form the chat line uses: 10s / 2m / 1h / 3d (truncating).")
}

func TestAttachmentSummary(t *testing.T) {
	t.Skip("TODO: attachmentSummary renders a message's attachments as a terse badge appended after the body: \"📎2圖\" (2 images), \"📎1檔\" (1 non-image file), or the mixed \"📎1圖 2檔\".")
}

func TestNewAckGate(t *testing.T) {
	t.Skip("TODO: newAckGate returns nil (⇒ the claude path) unless the parent asked for acks.")
}

func TestConfirm(t *testing.T) {
	t.Skip("TODO: confirm prints `[ocagent] listen: batch <token>` and waits for the verdict on that exact token.")
}

func TestNoteChatFetchFault(t *testing.T) {
	t.Skip("TODO: noteChatFetchFault announces a TOTAL fetch fault once per episode.")
}

func TestClearChatFetchFault(t *testing.T) {
	t.Skip("TODO: clearChatFetchFault closes the episode noteChatFetchFault opened, and SAYS SO.")
}

func TestDrainChat(t *testing.T) {
	t.Skip("TODO: drainChat refetches chat and prints the unread-for-me — ONE LINE per message so the spawned session's Monitor reads exactly '誰、多久前、說了什麼': [ocagent] chat from m-3417933c8632 (#c-ceb835093301, 2m ago): ...")
}

func TestReportChatRead(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestWarnMarkReadFailed(t *testing.T) {
	t.Skip("TODO: warnMarkReadFailed says ONCE per process that the read receipt did not land.")
}

func TestPrintChatLine(t *testing.T) {
	t.Skip("TODO: printChatLine emits the one-line-per-message form documented on drainChat.")
}

func TestHandleReplyCard(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- reply-card downlink (R7: the delta payload is a hint — refetch the authority).")
}

func TestPrintReplyCardAnswered(t *testing.T) {
	t.Skip("TODO: printReplyCardAnswered is the ONE answer line both the live delta path and the boot/reconnect drain emit — same wake, same shape, whichever path wins.")
}

func TestPrintReplyCardExpired(t *testing.T) {
	t.Skip("TODO: printReplyCardExpired is the ONE expiry line both the live delta path and the boot/reconnect drain emit — self-carrying guidance so an agent whose seeds predate the expired state still knows what to do: nobody answered (NOT a decision); reopen a FRESH card with current context if the question still matters, otherwise close out / proceed.")
}

func TestRenderReplyCardAnswer(t *testing.T) {
	t.Skip("TODO: renderReplyCardAnswer renders a card's answer as ONE terse fragment: EVERY circled option's ORIGINAL wording (an index alone is meaningless to a session), any typed text, and an attachment count.")
}

func TestReplyCardSeenPath(t *testing.T) {
	t.Skip("TODO: replyCardSeenPath is the state file, sibling of cursorPath.")
}

func TestLoadReplyCardSeen(t *testing.T) {
	t.Skip("TODO: loadReplyCardSeen reads the persisted state; a missing or corrupt file yields an UNPRIMED store (the first drain baselines silently).")
}

func TestHas(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestRecord(t *testing.T) {
	t.Skip("TODO: record marks one answer surfaced and persists immediately — the state must survive a kill that lands right after the print.")
}

func TestPersist(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestDrainReplyCards(t *testing.T) {
	t.Skip("TODO: drainReplyCards refetches the answered AND expired panes and prints MY not-yet-surfaced answers/expiries — the same lines the live handler emits, oldest first (per pane) so the session reads a chronology.")
}

func TestStrOrEmpty(t *testing.T) {
	t.Skip("TODO: strOrEmpty mirrors Python's str(x or \"\") idiom for a JSON-decoded value: nil / empty string / 0 / false → \"\" (Python-falsy); a string → itself; any other scalar → its natural text.")
}
