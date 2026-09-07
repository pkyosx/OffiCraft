// Skeleton generated from cli/ocagent/listen_run.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestNewSSEStreamClient(t *testing.T) {
	t.Skip("TODO: newSSEStreamClient builds the long-lived HTTP client for the SSE downlink.")
}

func TestLogf(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestFoldProbe(t *testing.T) {
	t.Skip("TODO: foldProbe runs ONE session-existence probe and folds its tri-state verdict into the self-exit debounce; returns true when the listener must self-exit.")
}

func TestFoldRefusal(t *testing.T) {
	t.Skip("TODO: foldRefusal folds ONE authoritative server refusal (pre-stream 409) into the fail-closed counter; returns true when BOTH bounds are crossed (see the sseRefusal* consts) and the listener must self-terminate.")
}

func TestResetRefusals(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestNoteDisconnect(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- the disconnect-notice policy (owner ruling, 2026-08-30) 「應該是在第一次斷線，跟連線回來的時候發訊息給 agent，中間的 retry 我們不需要 降低頻率，但是不需要打攪 agent。」 🔴 THIS IS NOT A BACKOFF CHANGE, AND MUST NEVER BECOME ONE.")
}

func TestStopRetrying(t *testing.T) {
	t.Skip("TODO: stopRetrying prints the give-up line when the retry loop terminates while an outage is still open, and returns the process exit code (always 0 — listen degrades gracefully).")
}

func TestStationVerdict(t *testing.T) {
	t.Skip("TODO: stationVerdict answers 「是不是換了一台」 on the reconnect line, so the reader is told rather than left to diff two shas by eye.")
}

func TestBaseAddressOrigin(t *testing.T) {
	t.Skip("TODO: baseAddressOrigin answers 「這個位址是誰決定的」 on the two transport lines a misconfigured listener actually reaches, and it is the whole of T-89.")
}

func TestDispatch(t *testing.T) {
	t.Skip("TODO: dispatch is the bridge from ONE completed SSE data payload to the agent's downlink behaviour: parse → echo gate → topic demux.")
}

func TestAuthoritativeRefusal(t *testing.T) {
	t.Skip("TODO: authoritativeRefusal names WHY a non-200 on /api/events is a STANDING \"you must not be online here\" — the only kind of failure that may accumulate toward the fail-closed self-terminate — or returns \"\" when it is not one.")
}

func TestConnectOnce(t *testing.T) {
	t.Skip("TODO: connectOnce dials GET /api/events (replaying from the persisted cursor via Last-Event-ID), and — on a 200 — streams the body through scanSSE until it ends.")
}

func TestDrainChatNow(t *testing.T) {
	t.Skip("TODO: drainChatNow is THE way this listener drains chat.")
}

func TestRun(t *testing.T) {
	t.Skip("TODO: run is the always-online listen loop.")
}

func TestSleepCtx(t *testing.T) {
	t.Skip("TODO: sleepCtx sleeps d via the injectable seam, treating a cancelled ctx as an immediate stop (checked before AND after).")
}

func TestCmdListen(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- production wiring — cmdListen: the realMain entrypoint for `ocagent listen`.")
}

func TestNewListener(t *testing.T) {
	t.Skip("TODO: newListener is the WIRING, pulled out of cmdListen so it can be asserted.")
}

func TestRootCtx(t *testing.T) {
	t.Skip("TODO: rootCtx gives run() a signal-driven root: SIGINT/SIGTERM cancels it and run() observes it to shut down GRACEFULLY — no hard kill of an in-flight SSE read.")
}
