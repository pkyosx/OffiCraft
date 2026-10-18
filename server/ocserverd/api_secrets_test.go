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

func (f apiTestSecretFixture) accessLog(t *testing.T, query string) []any {
	t.Helper()
	status, data := apiJSON(t, f.h, "GET", "/api/secrets/access-log"+query, f.owner, "")
	if status != http.StatusOK {
		t.Fatalf("access log: %d %v", status, data)
	}
	entries, _ := data["entries"].([]any)
	return entries
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
			"created_ts": apiAnyNumber, "updated_ts": apiAnyNumber,
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
		if _, err := os.Stat(f.keyPath); !os.IsNotExist(err) {
			t.Fatalf("a replacement key was minted: %v", err)
		}
	})
}

func TestHandleListSecretsApiSecretsGet(t *testing.T) {
	t.Run("secrets come back in creation order without values", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		first := f.create(t, "first", apiTestSecretValue, "n1")
		second := f.create(t, "second", "fake-value-2", "")
		rec := apiRequest(t, f.h, "GET", "/api/secrets", f.owner, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue, "fake-value-2")
		var data map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
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
	t.Run("an edit without a value keeps the stored ciphertext", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		id := f.create(t, "old", apiTestSecretValue, "keep me")["id"].(string)
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
	t.Run("delete returns the secret, removes the row and keeps its access log", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		created := f.create(t, "gone", apiTestSecretValue, "")
		id := created["id"].(string)
		if status, _ := f.reveal(t, f.agent, id); status != http.StatusOK {
			t.Fatalf("reveal: %d", status)
		}
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
		entries := f.accessLog(t, "?secret_id="+id)
		if len(entries) != 1 || entries[0].(map[string]any)["secret_name"] != "" {
			t.Fatalf("access log after delete = %v, want one row with secret_name \"\"", entries)
		}
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

func TestHandleListSecretAccessLogApiSecretsAccessLogGet(t *testing.T) {
	t.Run("entries are newest first and narrow by secret, member, action and limit", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "alpha", apiTestSecretValue, "")["id"].(string)
		b := f.create(t, "beta", "fake-value-2", "")["id"].(string)
		f.reveal(t, f.agent, a)
		f.reveal(t, f.admin, b)
		f.reveal(t, f.agent, "sec-000000000000")

		all := f.accessLog(t, "")
		if len(all) != 3 {
			t.Fatalf("entries = %v", all)
		}
		apiWantValue(t, "newest", all[0], map[string]any{
			"id": apiAnyNumber, "action": "denied", "secret_id": "sec-000000000000", "secret_name": "",
			"member_id": apiTestPlainAgentID, "machine_id": apiTestSecretMachineID, "ts": apiAnyNumber,
		})
		apiWantValue(t, "oldest", all[2], map[string]any{
			"id": apiAnyNumber, "action": "reveal", "secret_id": a, "secret_name": "alpha",
			"member_id": apiTestPlainAgentID, "machine_id": apiTestSecretMachineID, "ts": apiAnyNumber,
		})
		for query, want := range map[string]int{
			"?secret_id=" + b:                   1,
			"?member_id=" + apiTestPlainAgentID: 2,
			"?action=reveal":                    2,
			"?action=denied&member_id=kip":      1,
			"?limit=1":                          1,
			"?limit=0":                          3,
		} {
			if got := f.accessLog(t, query); len(got) != want {
				t.Fatalf("%s: %d entries, want %d", query, len(got), want)
			}
		}
		if status, _ := apiJSON(t, f.h, "GET", "/api/secrets/access-log?action=keep", f.owner, ""); status != http.StatusUnprocessableEntity {
			t.Fatalf("unknown action: %d, want 422", status)
		}
	})

	t.Run("only the owner may read the log", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		got := f.statuses(t, "GET", func() string { return "/api/secrets/access-log" }, "")
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

	t.Run("agents and admin agents may list; the owner and machines may not", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		got := f.statuses(t, "GET", func() string { return "/api/self/secrets" }, "")
		apiWantStatuses(t, got, map[string]int{"owner": 403, "admin_agent": 200, "agent": 200, "machine": 403})
	})
}

func TestHandleRevealSelfSecretsApiSelfSecretsRevealPost(t *testing.T) {
	t.Run("values come back in request order with one reveal row each", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "na")["id"].(string)
		b := f.create(t, "b", "fake-value-2", "")["id"].(string)
		status, data := f.reveal(t, f.agent, b, a)
		if status != http.StatusOK {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{"secrets": []any{
			map[string]any{"id": b, "name": "b", "note": "", "value": "fake-value-2"},
			map[string]any{"id": a, "name": "a", "note": "na", "value": apiTestSecretValue},
		}})
		entries := f.accessLog(t, "?action=reveal")
		if len(entries) != 2 {
			t.Fatalf("reveal rows = %v", entries)
		}
		for _, e := range entries {
			row := e.(map[string]any)
			if row["member_id"] != apiTestPlainAgentID || row["machine_id"] != apiTestSecretMachineID {
				t.Fatalf("reveal row does not name the caller: %v", row)
			}
		}
	})

	t.Run("one unknown id refuses the whole call and logs every asked id as denied", func(t *testing.T) {
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
		if got := f.accessLog(t, "?action=reveal"); len(got) != 0 {
			t.Fatalf("a refused call wrote reveal rows: %v", got)
		}
		denied := map[string]bool{}
		for _, e := range f.accessLog(t, "?action=denied") {
			denied[e.(map[string]any)["secret_id"].(string)] = true
		}
		if len(denied) != 2 || !denied[a] || !denied["sec-000000000000"] {
			t.Fatalf("denied rows name %v, want both asked ids", denied)
		}
	})

	t.Run("malformed id lists are refused and logged as denied", func(t *testing.T) {
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
		denied := f.accessLog(t, "?action=denied")
		// [] and {} log one blank row each, the duplicate body one row for
		// the repeated id, and the oversized body is capped at 16.
		if len(denied) != 1+16+1+1 {
			t.Fatalf("denied rows = %d", len(denied))
		}
	})

	t.Run("ids not shaped like secret ids are a 400 with one blank denied row", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		status, data := apiJSON(t, f.h, "POST", "/api/self/secrets/reveal", f.agent,
			`{"ids":["`+a+`","garbage-id-from-a-caller","SEC-AAAAAAAAAAAA"]}`)
		if status != http.StatusBadRequest {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "ids must be secret ids (sec- followed by 12 lowercase hex digits)")
		denied := f.accessLog(t, "?action=denied")
		if len(denied) != 1 || denied[0].(map[string]any)["secret_id"] != "" {
			t.Fatalf("denied rows = %v, want one row with secret_id \"\"", denied)
		}
	})

	t.Run("a token whose subject is not an active AI member is refused", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		ghost := apiTestAgentToken(t, f.api, "ghost-no-roster-row", "")
		rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", ghost, `{"ids":["`+a+`"]}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue)
		if got := f.accessLog(t, "?action=reveal"); len(got) != 0 {
			t.Fatalf("a refused caller wrote reveal rows: %v", got)
		}
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

	t.Run("the owner is refused and the attempt is logged; a machine never reaches the handler", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		body := `{"ids":["` + a + `"]}`
		got := f.statuses(t, "POST", func() string { return "/api/self/secrets/reveal" }, body)
		apiWantStatuses(t, got, map[string]int{"owner": 403, "admin_agent": 200, "agent": 200, "machine": 403})
		denied := f.accessLog(t, "?action=denied")
		if len(denied) != 1 || denied[0].(map[string]any)["member_id"] != wireOwnerID {
			t.Fatalf("denied rows = %v, want the owner's one", denied)
		}
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

	t.Run("a ciphertext moved onto another id does not decrypt", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		a := f.create(t, "a", apiTestSecretValue, "")["id"].(string)
		b := f.create(t, "b", "fake-value-2", "")["id"].(string)
		src := f.row(t, a)
		if _, err := f.d.wdb.Exec(`UPDATE secret SET nonce = ?, value_ct = ? WHERE id = ?`, src.Nonce, src.ValueCT, b); err != nil {
			t.Fatal(err)
		}
		rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", f.agent, `{"ids":["`+b+`"]}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		apiWantNoSecretValue(t, rec.Body.Bytes(), apiTestSecretValue, "fake-value-2")
		if got := f.accessLog(t, "?action=reveal"); len(got) != 0 {
			t.Fatalf("a failed decrypt wrote reveal rows: %v", got)
		}
	})
}
