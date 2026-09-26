package main

import "strings"

// workerSharedHead mirrors the first two blocks of the staff fold
// (buildBootContext): 系統互動, then 使用者自訂 only when the owner text is
// non-blank. The 啟動步驟 block is NOT here: it is the recency-authoritative tail and is
// appended last by buildWorkerBootContext.
func (s *apiServer) workerSharedHead() (string, error) {
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

// workerBootSequence must use the worker's OWN runtime, not the Claude seed:
// only the codex boot sequence ends the boot turn by handing control back to
// the sidecar that holds its connection.
func (s *apiServer) workerBootSequence(runtime string) (string, error) {
	boot, err := s.bootSequenceText(runtime)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(boot), nil
}
