// Skeleton generated from server/ocserverd/api_backup_health.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHandleGetBackupHealthApiBackupHealthGet(t *testing.T) {
	t.Run("a server whose watchdog was never armed reports unknown rather than an error", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/backup-health", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":           "unknown",
			"code":             "",
			"detail":           "backup health is not being watched on this server",
			"stale_after_secs": 43200,
		})
		dashboard.wantFrames()
	})

	t.Run("an armed watchdog that has not evaluated yet is still unknown, and says which kind of unknown", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.backupHealth = newBackupHealthMonitor(d, t.TempDir()+"/backup.db")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/backup-health", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":           "unknown",
			"code":             "",
			"detail":           "the backup watchdog has not reported yet",
			"stale_after_secs": 43200,
		})
		dashboard.wantFrames()
	})

	t.Run("a durable healthy verdict is served through with its stamps and the age derived from them", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.backupHealth = newBackupHealthMonitor(d, t.TempDir()+"/backup.db")
		if err := api.backupHealth.save(backupHealthState{
			CheckedTS: 1788000000, NewestBackupTS: 1787990000,
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/backup-health", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":                 "healthy",
			"code":                   "",
			"detail":                 "",
			"checked_ts":             1788000000,
			"newest_backup_ts":       1787990000,
			"newest_backup_age_secs": 10000,
			"stale_after_secs":       43200,
		})
		dashboard.wantFrames()
	})

	t.Run("a durable incident is served as unhealthy, carrying its code, its detail and when it started", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.backupHealth = newBackupHealthMonitor(d, t.TempDir()+"/backup.db")
		if err := api.backupHealth.save(backupHealthState{
			Code: "stale", Detail: "the newest scheduled backup is too old",
			SinceTS: 1787900000, CheckedTS: 1788000000, NewestBackupTS: 1787990000,
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/backup-health", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":                 "unhealthy",
			"code":                   "stale",
			"detail":                 "the newest scheduled backup is too old",
			"since_ts":               1787900000,
			"checked_ts":             1788000000,
			"newest_backup_ts":       1787990000,
			"newest_backup_age_secs": 10000,
			"stale_after_secs":       43200,
		})
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/backup-health", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/backup-health", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a GET /api/backup-health request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) {
		t.Skip("structurally unproducible: this GET decodes no request body and the " +
			"stack carries no content-type or size middleware, so no wire-layer 4xx " +
			"exists to observe — measured: a `{{{` body on this route still answers 200.")
	})
}
