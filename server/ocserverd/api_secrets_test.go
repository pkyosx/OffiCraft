package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	apiTestSecretValue     = "fake-value-1"
	apiTestSecretMachineID = "m-secret-host"
)

type apiTestSecretFixture struct {
	api     *apiServer
	h       http.Handler
	d       *DAL
	keyPath string
	owner   string
	admin   string
	agent   string
	machine string
}

func newAPITestSecretFixture(t *testing.T) apiTestSecretFixture {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	keyPath := filepath.Join(t.TempDir(), vaultKeyFileName)
	api.vault = newSecretVault(keyPath)
	return apiTestSecretFixture{
		api: api, h: h, d: d, keyPath: keyPath, owner: owner,
		admin:   apiTestPrincipalToken(t, api, d, principalAdminAgent, "secret-admin"),
		agent:   apiTestAgentToken(t, api, apiTestPlainAgentID, apiTestSecretMachineID),
		machine: apiTestPrincipalToken(t, api, d, principalMachine, "secret-warden"),
	}
}

func (f apiTestSecretFixture) create(t *testing.T, name, value, note string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "value": value, "note": note})
	status, data := apiJSON(t, f.h, "POST", "/api/secrets", f.owner, string(body))
	if status != http.StatusOK {
		t.Fatalf("create secret: %d %v", status, data)
	}
	return data
}

func (f apiTestSecretFixture) reveal(t *testing.T, token string, ids ...string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"ids": ids})
	return apiJSON(t, f.h, "POST", "/api/self/secrets/reveal", token, string(body))
}

// notActiveAIMembers answers credentials whose subject is not an active AI
// member, with the status each is refused with: a subject with no roster row
// passes the gate, a removed member is refused there.
func (f apiTestSecretFixture) notActiveAIMembers(t *testing.T) []struct {
	name, token string
	status      int
} {
	t.Helper()
	removed := Member{ID: "secret-removed", Name: "secret-removed", Kind: KindStaff, RoleKey: "conf-plain-role", RosterStatus: RosterStatusRemoved}
	if err := f.d.putMemberWholeRowForTest(removed); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	return []struct {
		name, token string
		status      int
	}{
		{"no roster row", apiTestAgentToken(t, f.api, "ghost-no-roster-row", ""), http.StatusForbidden},
		{"removed member", apiTestAgentToken(t, f.api, removed.ID, ""), http.StatusUnauthorized},
	}
}

func (f apiTestSecretFixture) row(t *testing.T, id string) Secret {
	t.Helper()
	sec, err := getSecretOn(f.d.rdb, id)
	if err != nil || sec == nil {
		t.Fatalf("secret row %s: %v %v", id, sec, err)
	}
	return *sec
}

// statuses answers each principal's status on one route; body may be a func so
// a mutating route gets a fresh target per caller.
func (f apiTestSecretFixture) statuses(t *testing.T, method string, path func() string, body string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, who := range []struct{ name, token string }{
		{"owner", f.owner}, {"admin_agent", f.admin}, {"agent", f.agent}, {"machine", f.machine},
	} {
		out[who.name] = apiRequest(t, f.h, method, path(), who.token, body).Code
	}
	return out
}

func (f apiTestSecretFixture) wantNeverAccessed(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if ts := f.row(t, id).LastAccessedTS; ts != nil {
			t.Fatalf("%s last_accessed_ts = %v, want it never set", id, *ts)
		}
	}
}

func apiWantStatuses(t *testing.T, got, want map[string]int) {
	t.Helper()
	for who, code := range want {
		if got[who] != code {
			t.Fatalf("%s answered %d, want %d (all: %v)", who, got[who], code, got)
		}
	}
}

func apiWantNoSecretValue(t *testing.T, raw []byte, values ...string) {
	t.Helper()
	for _, v := range values {
		if bytes.Contains(raw, []byte(v)) {
			t.Fatalf("response carries a secret value: %s", raw)
		}
	}
	if bytes.Contains(raw, []byte(`"value"`)) || bytes.Contains(raw, []byte("value_ct")) {
		t.Fatalf("response carries a value field: %s", raw)
	}
}

var apiTestSecretIDPattern = regexp.MustCompile(`^sec-[0-9a-f]{12}$`)

func TestHandleCreateSecretApiSecretsPost(t *testing.T) {
	t.Run("the created secret comes back without its value and is sealed at rest", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		rec := apiRequest(t, f.h, "POST", "/api/secrets", f.owner,
			`{"name":"  GitHub token  ","value":"`+apiTestSecretValue+`","note":"read-only scope"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		var data map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
		apiWantBody(t, data, map[string]any{
			"id": apiAnyString, "name": "GitHub token", "note": "read-only scope",
			"created_ts": apiAnyNumber, "updated_ts": apiAnyNumber, "last_accessed_ts": nil,
		})
		id := data["id"].(string)
		if !apiTestSecretIDPattern.MatchString(id) {
			t.Fatalf("id %q is not sec- + 12 hex", id)
		}
		sec := f.row(t, id)
		if bytes.Contains(sec.ValueCT, []byte(apiTestSecretValue)) || len(sec.Nonce) != vaultNonceBytes || sec.KeyVersion != 1 {
			t.Fatalf("row is not sealed: nonce=%d bytes key_version=%d", len(sec.Nonce), sec.KeyVersion)
		}
	})

	t.Run("the first create mints a 0600 key file beside the database", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		if _, err := os.Stat(f.keyPath); !os.IsNotExist(err) {
			t.Fatalf("premise: no key before the first secret, got %v", err)
		}
		f.create(t, "a", apiTestSecretValue, "")
		st, err := os.Stat(f.keyPath)
		if err != nil {
			t.Fatalf("stat key: %v", err)
		}
		if st.Mode().Perm() != 0o600 || st.Size() != vaultKeyBytes {
			t.Fatalf("key file mode %o size %d, want 600 and %d", st.Mode().Perm(), st.Size(), vaultKeyBytes)
		}
	})

	t.Run("invalid fields are refused naming the field and limit, never the value", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		long := strings.Repeat("v", secretValueMaxChars+1)
		for _, tc := range []struct {
			body, msg string
			status    int
		}{
			{`{"name":"  ","value":"x"}`, "name must not be blank", 400},
			{`{"name":"` + strings.Repeat("名", 129) + `","value":"x"}`, "name is 129 chars, over the 128-char limit", 400},
			{`{"name":"n","value":""}`, "value must not be empty", 400},
			{`{"name":"n","value":"` + long + `"}`, "value is over the 65536-char limit", 400},
			{`{"name":"n","value":"x","note":"` + strings.Repeat("n", 4001) + `"}`, "note is 4001 chars, over the 4000-char limit", 400},
			{`{"name":"n"}`, "field required: value", 422},
		} {
			status, data := apiJSON(t, f.h, "POST", "/api/secrets", f.owner, tc.body)
			if status != tc.status {
				t.Fatalf("%.40s: status %d, want %d", tc.body, status, tc.status)
			}
			apiWantError(t, data, "validation_error", tc.msg)
		}
		status, _ := apiJSON(t, f.h, "POST", "/api/secrets", f.owner,
			`{"name":"`+strings.Repeat("名", shortLabelMaxChars)+`","value":"`+strings.Repeat("名", secretValueMaxChars)+
				`","note":"`+strings.Repeat("n", chatBodyMaxChars)+`"}`)
		if status != http.StatusOK {
			t.Fatalf("a name, value and note each exactly at its limit must be accepted, got %d", status)
		}
	})

	t.Run("names are not unique", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "same", apiTestSecretValue, "")
		b := f.create(t, "same", apiTestSecretValue, "")
		if a["id"] == b["id"] {
			t.Fatalf("two creates minted the same id %v", a["id"])
		}
	})

	t.Run("only the owner may create", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		got := f.statuses(t, "POST", func() string { return "/api/secrets" }, `{"name":"n","value":"x"}`)
		apiWantStatuses(t, got, map[string]int{"owner": 200, "admin_agent": 403, "agent": 403, "machine": 403})
	})

	t.Run("a missing key with rows already sealed answers 503 instead of minting a new key", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		f.create(t, "a", apiTestSecretValue, "")
		if err := os.Remove(f.keyPath); err != nil {
			t.Fatal(err)
		}
		f.api.vault = newSecretVault(f.keyPath)
		status, data := apiJSON(t, f.h, "POST", "/api/secrets", f.owner, `{"name":"b","value":"fake-value-2"}`)
		if status != http.StatusServiceUnavailable {
			t.Fatalf("status %d %v, want 503", status, data)
		}
		if msg, _ := data["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "Restore vault.key") ||
			!strings.Contains(msg, "delete the existing secrets and recreate them") {
			t.Fatalf("message %q does not say how to recover", msg)
		}
		if _, err := os.Stat(f.keyPath); !os.IsNotExist(err) {
			t.Fatalf("a replacement key was minted: %v", err)
		}
	})
}

func TestHandleListSecretsApiSecretsGet(t *testing.T) {
	t.Run("secrets come back in creation order without values, with when each was last revealed", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		first := f.create(t, "first", apiTestSecretValue, "n1")
		second := f.create(t, "second", "fake-value-2", "")
		if status, _ := f.reveal(t, f.agent, first["id"].(string)); status != http.StatusOK {
			t.Fatalf("reveal: %d", status)
		}
		rec := apiRequest(t, f.h, "GET", "/api/secrets", f.owner, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue, "fake-value-2")
		var data map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
		first["last_accessed_ts"] = apiAnyNumber
		apiWantBody(t, data, map[string]any{"secrets": []any{first, second}})
	})

	t.Run("an empty vault is an empty list", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		_, data := apiJSON(t, f.h, "GET", "/api/secrets", f.owner, "")
		apiWantBody(t, data, map[string]any{"secrets": []any{}})
	})

	t.Run("only the owner may list", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		got := f.statuses(t, "GET", func() string { return "/api/secrets" }, "")
		apiWantStatuses(t, got, map[string]int{"owner": 200, "admin_agent": 403, "agent": 403, "machine": 403})
	})
}

func TestHandleUpdateSecretApiSecretsSecretIdPatch(t *testing.T) {
	t.Run("an edit without a value keeps the stored ciphertext and the last access time", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "old", apiTestSecretValue, "keep me")["id"].(string)
		if status, _ := f.reveal(t, f.agent, id); status != http.StatusOK {
			t.Fatalf("reveal: %d", status)
		}
		before := f.row(t, id)
		rec := apiRequest(t, f.h, "PATCH", "/api/secrets/"+id, f.owner, `{"name":" new ","value":null}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		after := f.row(t, id)
		if after.Name != "new" || after.Note != "keep me" {
			t.Fatalf("name/note = %q/%q", after.Name, after.Note)
		}
		if !bytes.Equal(after.ValueCT, before.ValueCT) || !bytes.Equal(after.Nonce, before.Nonce) {
			t.Fatal("ciphertext changed though no value was sent")
		}
		if after.CreatedTS != before.CreatedTS || after.UpdatedTS <= before.UpdatedTS {
			t.Fatalf("timestamps: created %v→%v updated %v→%v", before.CreatedTS, after.CreatedTS, before.UpdatedTS, after.UpdatedTS)
		}
		if before.LastAccessedTS == nil || after.LastAccessedTS == nil || *after.LastAccessedTS != *before.LastAccessedTS {
			t.Fatalf("last_accessed_ts %v→%v, want it kept across an edit", before.LastAccessedTS, after.LastAccessedTS)
		}
	})

	t.Run("a new value is re-sealed with a fresh nonce and is what reveal returns", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "n", apiTestSecretValue, "")["id"].(string)
		before := f.row(t, id)
		status, _ := apiJSON(t, f.h, "PATCH", "/api/secrets/"+id, f.owner, `{"value":"fake-value-2"}`)
		if status != http.StatusOK {
			t.Fatalf("status %d", status)
		}
		if bytes.Equal(f.row(t, id).Nonce, before.Nonce) {
			t.Fatal("nonce was reused")
		}
		_, data := f.reveal(t, f.agent, id)
		apiWantBody(t, data, map[string]any{"secrets": []any{
			map[string]any{"id": id, "name": "n", "note": "", "value": "fake-value-2"},
		}})
	})

	t.Run("an empty note clears it", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "n", apiTestSecretValue, "old note")["id"].(string)
		_, data := apiJSON(t, f.h, "PATCH", "/api/secrets/"+id, f.owner, `{"note":""}`)
		if data["note"] != "" {
			t.Fatalf("note = %v", data["note"])
		}
	})

	t.Run("an unknown id or an invalid field is refused", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "n", apiTestSecretValue, "")["id"].(string)
		status, data := apiJSON(t, f.h, "PATCH", "/api/secrets/sec-nope", f.owner, `{"note":"x"}`)
		if status != http.StatusNotFound {
			t.Fatalf("status %d", status)
		}
		apiWantError(t, data, "not_found", "secret 'sec-nope' not found")
		for body, msg := range map[string]string{
			`{"name":""}`:  "name must not be blank",
			`{"value":""}`: "value must not be empty",
			`{"note":"` + strings.Repeat("n", 4001) + `"}`: "note is 4001 chars, over the 4000-char limit",
		} {
			status, data := apiJSON(t, f.h, "PATCH", "/api/secrets/"+id, f.owner, body)
			if status != http.StatusBadRequest {
				t.Fatalf("%.30s: status %d", body, status)
			}
			apiWantError(t, data, "validation_error", msg)
		}
	})

	t.Run("only the owner may edit", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "n", apiTestSecretValue, "")["id"].(string)
		got := f.statuses(t, "PATCH", func() string { return "/api/secrets/" + id }, `{"note":"x"}`)
		apiWantStatuses(t, got, map[string]int{"owner": 200, "admin_agent": 403, "agent": 403, "machine": 403})
	})
}

func TestHandleDeleteSecretApiSecretsSecretIdDelete(t *testing.T) {
	t.Run("delete returns the secret and removes the row", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		created := f.create(t, "gone", apiTestSecretValue, "")
		id := created["id"].(string)
		rec := apiRequest(t, f.h, "DELETE", "/api/secrets/"+id, f.owner, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		var data map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
		apiWantBody(t, data, created)
		_, list := apiJSON(t, f.h, "GET", "/api/secrets", f.owner, "")
		apiWantBody(t, list, map[string]any{"secrets": []any{}})
		if status, _ := apiJSON(t, f.h, "DELETE", "/api/secrets/"+id, f.owner, ""); status != http.StatusNotFound {
			t.Fatalf("second delete: %d, want 404", status)
		}
	})

	t.Run("only the owner may delete", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		path := func() string { return "/api/secrets/" + f.create(t, "n", apiTestSecretValue, "")["id"].(string) }
		got := f.statuses(t, "DELETE", path, "")
		apiWantStatuses(t, got, map[string]int{"owner": 200, "admin_agent": 403, "agent": 403, "machine": 403})
	})
}

func TestHandleListSelfSecretsApiSelfSecretsGet(t *testing.T) {
	t.Run("an AI member sees id, name and note only", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "deploy key", apiTestSecretValue, "staging only")["id"].(string)
		rec := apiRequest(t, f.h, "GET", "/api/self/secrets", f.agent, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		var data map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
		apiWantBody(t, data, map[string]any{"secrets": []any{
			map[string]any{"id": id, "name": "deploy key", "note": "staging only"},
		}})
	})

	t.Run("active agents and admin agents may list; the owner, machines and callers off the active roster may not", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		got := f.statuses(t, "GET", func() string { return "/api/self/secrets" }, "")
		apiWantStatuses(t, got, map[string]int{"owner": 403, "admin_agent": 200, "agent": 200, "machine": 403})
		for _, c := range f.notActiveAIMembers(t) {
			if code := apiRequest(t, f.h, "GET", "/api/self/secrets", c.token, "").Code; code != c.status {
				t.Fatalf("%s answered %d, want %d", c.name, code, c.status)
			}
		}
	})
}

func TestHandleRevealSelfSecretsApiSelfSecretsRevealPost(t *testing.T) {
	t.Run("values come back in request order and every revealed secret records the access time", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "na")["id"].(string)
		b := f.create(t, "b", "fake-value-2", "")["id"].(string)
		untouched := f.create(t, "c", "fake-value-3", "")["id"].(string)
		before := nowSecs()
		status, data := f.reveal(t, f.agent, b, a)
		if status != http.StatusOK {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{"secrets": []any{
			map[string]any{"id": b, "name": "b", "note": "", "value": "fake-value-2"},
			map[string]any{"id": a, "name": "a", "note": "na", "value": apiTestSecretValue},
		}})
		for _, id := range []string{a, b} {
			if ts := f.row(t, id).LastAccessedTS; ts == nil || *ts < before {
				t.Fatalf("%s last_accessed_ts = %v, want at or after %v", id, ts, before)
			}
		}
		f.wantNeverAccessed(t, untouched)
	})

	t.Run("one unknown id refuses the whole call and records no access", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", f.agent,
			`{"ids":["`+a+`","sec-000000000000"]}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status %d", rec.Code)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		var data map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
		apiWantError(t, data, "not_found", "unknown secret id(s): sec-000000000000")
		f.wantNeverAccessed(t, a)
	})

	t.Run("malformed id lists are refused", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		seventeen := make([]string, 17)
		for i := range seventeen {
			seventeen[i] = fmt.Sprintf("sec-%012d", i)
		}
		many, _ := json.Marshal(map[string]any{"ids": seventeen})
		for _, tc := range []struct{ body, msg string }{
			{`{"ids":[]}`, "ids must name 1–16 secrets"},
			{string(many), "ids must name 1–16 secrets"},
			{`{"ids":["sec-aaaaaaaaaaaa","sec-aaaaaaaaaaaa"]}`, "ids must be unique: 'sec-aaaaaaaaaaaa' appears more than once"},
			{`{}`, "field required: ids"},
		} {
			status, data := apiJSON(t, f.h, "POST", "/api/self/secrets/reveal", f.agent, tc.body)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%.30s: status %d", tc.body, status)
			}
			apiWantError(t, data, "validation_error", tc.msg)
		}
	})

	t.Run("ids not shaped like secret ids are a 400 that records no access", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		status, data := apiJSON(t, f.h, "POST", "/api/self/secrets/reveal", f.agent,
			`{"ids":["`+a+`","garbage-id-from-a-caller","SEC-AAAAAAAAAAAA"]}`)
		if status != http.StatusBadRequest {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "ids must be secret ids (sec- followed by 12 lowercase hex digits)")
		f.wantNeverAccessed(t, a)
	})

	t.Run("a token whose subject is not an active AI member is refused and records no access", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		for _, c := range f.notActiveAIMembers(t) {
			rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", c.token, `{"ids":["`+a+`"]}`)
			if rec.Code != c.status {
				t.Fatalf("%s: status %d, want %d: %s", c.name, rec.Code, c.status, rec.Body)
			}
			apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		}
		f.wantNeverAccessed(t, a)
	})

	t.Run("a multi-line value round-trips byte for byte", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		multi := "-----BEGIN FAKE KEY-----\nfake-line-1\r\nfake-line-2\n\n-----END FAKE KEY-----\n"
		id := f.create(t, "deploy key", multi, "")["id"].(string)
		_, data := f.reveal(t, f.agent, id)
		got := data["secrets"].([]any)[0].(map[string]any)["value"]
		if got != multi {
			t.Fatalf("revealed %q, want %q", got, multi)
		}
	})

	t.Run("the owner and machines are refused without recording an access; AI members may reveal", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		body := `{"ids":["` + a + `"]}`
		for _, token := range []string{f.owner, f.machine} {
			if code := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", token, body).Code; code != http.StatusForbidden {
				t.Fatalf("status %d, want 403", code)
			}
		}
		f.wantNeverAccessed(t, a)
		got := f.statuses(t, "POST", func() string { return "/api/self/secrets/reveal" }, body)
		apiWantStatuses(t, got, map[string]int{"owner": 403, "admin_agent": 200, "agent": 200, "machine": 403})
	})

	t.Run("a missing key file is a 503, never a value", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		if err := os.Remove(f.keyPath); err != nil {
			t.Fatal(err)
		}
		f.api.vault = newSecretVault(f.keyPath)
		rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", f.agent, `{"ids":["`+a+`"]}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", rec.Code)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
	})

	t.Run("a ciphertext moved onto another id does not decrypt, and the readable secret revealed alongside it stays never accessed", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		b := f.create(t, "b", "fake-value-2", "")["id"].(string)
		src := f.row(t, a)
		if _, err := f.d.wdb.Exec(`UPDATE secret SET nonce = ?, value_ct = ? WHERE id = ?`, src.Nonce, src.ValueCT, b); err != nil {
			t.Fatal(err)
		}
		rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", f.agent, `{"ids":["`+a+`","`+b+`"]}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue, "fake-value-2")
		f.wantNeverAccessed(t, a, b)
	})
}
