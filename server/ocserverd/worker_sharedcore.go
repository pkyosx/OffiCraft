package main

import "strings"

// ── shared core / Global Context ───────────────────────────────────────────
//
// "Global Context" is what the cockpit shows under 全域情境 — THREE blocks:
//
//	1. 系統互動   /api/system-interaction       (owner-editable; folds to the
//	                                            seed when unedited — T-791e)
//	2. 使用者自訂 /api/global-context           (owner-editable additive block)
//	3. 啟動程序   /api/boot-sequence/{runtime_key}
//	                                            (owner-editable studio SOP,
//	                                            per-runtime; same fold — see
//	                                            below)
//
// Members and workers receive all three, IN THE SAME SLOTS (T-4595): 系統互動
// first, 使用者自訂 second, 啟動程序 last, with the reader's own persona
// wedged in between (staff: 角色說明 → 判準（空白則整段跳過）→ 長期筆記;
// outsource: nothing at all —
// it has no role, and that empty slot is the ENTIRE difference between the two
// documents).
// The seed blocks deliberately remain byte-for-byte shared; nothing is filtered
// or rewritten for either audience. There is no outsource-only seed at all —
// seeds/worker_context.md was deleted by T-4595 after every line in it turned
// out to be either false, a restatement of the shared seed, or a difference
// nobody could name a harm for.
//
// "Byte-for-byte shared" is per RUNTIME, not global: the 啟動程序 block is the
// boot sequence of the runtime the reader is actually running
// (bootSequenceSeedName, assets.go), and staff and outsource on the SAME runtime
// get the same bytes. Handing every worker the Claude seed is not parity — it is
// how a codex worker ended up being told to run its own `ocagent listen`.

// workerSharedHead returns the FIRST TWO shared blocks of a worker boot context
// — 系統互動 then 使用者自訂 — in the same order and with the same rule the
// member fold uses.
//
// The 使用者自訂 block follows the member rule: skipped entirely when the owner
// text is blank, so a worker never sees an empty header.
//
// The 啟動程序 block is NOT here: it is the recency-authoritative tail and is
// appended last by buildWorkerBootContext, exactly as buildBootContext does for
// staff. Grouping all three at the top (the pre-T-4595 shape) put the studio
// SOP ABOVE the persona for workers and BELOW it for staff — one asymmetry with
// nothing behind it.
func (s *apiServer) workerSharedHead() (string, error) {
	// The FOLDED block (T-791e): the owner's edit when there is one, the shipped
	// seed otherwise — the same call the staff fold makes, so an edited
	// 系統互動 reaches workers and staff as one document rather than two.
	sys, err := s.systemInteractionText()
	if err != nil {
		return "", err
	}
	parts := []string{strings.TrimSpace(sys)}

	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			userAdditionsTitle+"\n\n"+strings.TrimSpace(userCtx.Text))
	}

	return strings.Join(parts, "\n\n"), nil
}

// workerBootSequence returns the 啟動程序 block for a worker's OWN runtime.
//
// runtime is the worker's OWN runtime (OutsourceWorker.Runtime). The
// boot-sequence seed is chosen from it through bootSequenceSeedName — the same
// single expression the staff fold uses — because parity with staff means "read
// the seed for the runtime you are running", not "everyone reads the Claude
// one". A codex worker handed boot_sequence.md is told to run a bare `ocagent
// listen` under Monitor, which directly contradicts the codex runtime tail its
// spawn appends.
func (s *apiServer) workerBootSequence(runtime string) (string, error) {
	// Folded, exactly like the staff tail (T-791e), and still chosen through the
	// one runtime decision point: bootSequenceText derives the document key from
	// bootSequenceSeedName rather than testing the runtime a second time.
	boot, err := s.bootSequenceText(runtime)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(boot), nil
}
