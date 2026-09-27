package main

// api_backup_health.go — a THIN read of the watchdog's durable verdict, so no two
// surfaces can disagree; see backup_health.go for why the watchdog is the only
// evaluator.

import "net/http"

// An unarmed watchdog reports `unknown` rather than 404/500: "we cannot tell" must
// be renderable, not read as "nothing to report".
func (s *apiServer) HandleGetBackupHealthApiBackupHealthGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.backupHealth.report())
}
