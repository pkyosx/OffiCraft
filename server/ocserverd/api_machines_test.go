// Skeleton generated from server/ocserverd/api_machines.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestMint(t *testing.T) {
	t.Run("each mint answers a fresh base64url code bound to the machine it names", func(t *testing.T) {
		store := newMachineClaimStore()
		now := time.Unix(1700000000, 0)

		first, err := store.mint("m-studio", now)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		second, err := store.mint("m-loft", now)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		if first == second {
			t.Fatalf("two mints answered the same code: %q", first)
		}
		for _, code := range []string{first, second} {
			if len(code) != 43 {
				t.Fatalf("code %q is %d characters", code, len(code))
			}
			if strings.Trim(code, apiTestBase64URLAlphabet) != "" {
				t.Fatalf("code %q leaves the base64url alphabet", code)
			}
		}
		apiWantValue(t, "pending claims", any(apiTestClaimBindings(t, store)),
			any(map[string]any{first: "m-studio", second: "m-loft"}))
	})

	t.Run("minting sweeps the codes that expired and leaves the live ones pending", func(t *testing.T) {
		store := newMachineClaimStore()
		start := time.Unix(1700000000, 0)
		abandoned, err := store.mint("m-abandoned", start)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		live, err := store.mint("m-live", start.Add(599*time.Second))
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		fresh, err := store.mint("m-fresh", start.Add(601*time.Second))
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		apiWantValue(t, "pending claims", any(apiTestClaimBindings(t, store)),
			any(map[string]any{live: "m-live", fresh: "m-fresh"}))
		if id, ok := store.take(abandoned, start.Add(601*time.Second)); ok {
			t.Fatalf("a swept code stayed redeemable: (%q, %v)", id, ok)
		}
	})

	t.Run("a code is still redeemable at the last instant of its ten-minute life", func(t *testing.T) {
		store := newMachineClaimStore()
		start := time.Unix(1700000000, 0)
		code, err := store.mint("m-studio", start)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		id, ok := store.take(code, start.Add(time.Duration(machineClaimTTLSecs)*time.Second))

		if id != "m-studio" || !ok {
			t.Fatalf(`want ("m-studio", true), got (%q, %v)`, id, ok)
		}
	})
}

// apiTestBase64URLAlphabet is every character a minted claim code may contain.
const apiTestBase64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"abcdefghijklmnopqrstuvwxyz0123456789-_"

// apiTestClaimBindings is the WHOLE pending claim table — the store's only
// observable — read under the same lock mint and take hold.
func apiTestClaimBindings(t *testing.T, st *machineClaimStore) map[string]any {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string]any{}
	for code, claim := range st.codes {
		out[code] = claim.machineID
	}
	return out
}

func TestTake(t *testing.T) {
	t.Run("a live code answers the machine it was bound to and leaves the table without it", func(t *testing.T) {
		store := newMachineClaimStore()
		now := time.Unix(1700000000, 0)
		code, err := store.mint("m-studio", now)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		other, err := store.mint("m-loft", now)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		id, ok := store.take(code, now)

		if id != "m-studio" || !ok {
			t.Fatalf(`want ("m-studio", true), got (%q, %v)`, id, ok)
		}
		apiWantValue(t, "pending claims", any(apiTestClaimBindings(t, store)),
			any(map[string]any{other: "m-loft"}))
	})

	t.Run("the same code a second time answers nothing because the first redemption deleted it", func(t *testing.T) {
		store := newMachineClaimStore()
		now := time.Unix(1700000000, 0)
		code, err := store.mint("m-studio", now)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if _, ok := store.take(code, now); !ok {
			t.Fatalf("the first redemption must succeed")
		}

		id, ok := store.take(code, now)

		if id != "" || ok {
			t.Fatalf(`want ("", false), got (%q, %v)`, id, ok)
		}
		apiWantValue(t, "pending claims", any(apiTestClaimBindings(t, store)), any(map[string]any{}))
	})

	t.Run("an expired code answers nothing and is dropped rather than left behind", func(t *testing.T) {
		store := newMachineClaimStore()
		start := time.Unix(1700000000, 0)
		code, err := store.mint("m-studio", start)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		id, ok := store.take(code, start.Add(601*time.Second))

		if id != "" || ok {
			t.Fatalf(`want ("", false), got (%q, %v)`, id, ok)
		}
		apiWantValue(t, "pending claims", any(apiTestClaimBindings(t, store)), any(map[string]any{}))
	})

	t.Run("a code nothing ever minted answers nothing and leaves the table whole", func(t *testing.T) {
		store := newMachineClaimStore()
		now := time.Unix(1700000000, 0)
		code, err := store.mint("m-studio", now)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}

		id, ok := store.take("nothing-was-ever-minted-here", now)

		if id != "" || ok {
			t.Fatalf(`want ("", false), got (%q, %v)`, id, ok)
		}
		apiWantValue(t, "pending claims", any(apiTestClaimBindings(t, store)),
			any(map[string]any{code: "m-studio"}))
	})
}

func TestBuildInstallScript(t *testing.T) {
	t.Run("a main-instance script is the exact text GET /install.sh serves for a legacy token link", func(t *testing.T) {
		got := buildInstallScript("https://example.com", "legacy-credential-placeholder", "")

		if got != apiTestLegacyInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})

	t.Run("a namespaced instance prefixes the install line with OC_NAMESPACE and templates the base into every line that names it", func(t *testing.T) {
		got := buildInstallScript("http://localhost:8848", "legacy-credential-placeholder", "bench")

		if got != apiTestNamespacedLegacyInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})
}

func TestBuildInstallScriptWithCode(t *testing.T) {
	t.Run("a main-instance script is the exact text GET /install.sh serves for a claim-code link", func(t *testing.T) {
		got := buildInstallScriptWithCode("https://example.com", "one-time-claim-code-placeholder", "")

		if got != apiTestClaimCodeInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})

	t.Run("a namespaced instance prefixes the install line with OC_NAMESPACE and templates the base into every line that names it", func(t *testing.T) {
		got := buildInstallScriptWithCode("http://localhost:8848", "one-time-claim-code-placeholder", "bench")

		if got != apiTestNamespacedClaimCodeInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})
}

func TestMachineBinStatus(t *testing.T) {
	t.Run("a machine that has never heartbeated has no verdict, on the wire too", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.binHashes = map[string]string{"ocwarden": "aaaa1111", "ocagent": "bbbb2222"}
		machineID, _ := apiTestMachineCredential(t, h, owner, "Studio Mac")

		apiWantValue(t, "bin_status", apiTestDeref(api.machineBinStatus(machineID)), nil)
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", nil)))
	})

	t.Run("a heartbeat matching every embedded fingerprint reads current, on the wire too", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.binHashes = map[string]string{"ocwarden": "aaaa1111", "ocagent": "bbbb2222"}
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"binaries":{"ocwarden":"aaaa1111","ocagent":"bbbb2222"}}`)

		apiWantValue(t, "bin_status", apiTestDeref(api.machineBinStatus(machineID)), "current")
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"bin_status": "current",
			})))
	})

	t.Run("one differing fingerprint reads stale even while the other still matches", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.binHashes = map[string]string{"ocwarden": "aaaa1111", "ocagent": "bbbb2222"}
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"binaries":{"ocwarden":"aaaa1111","ocagent":"cccc3333"}}`)

		apiWantValue(t, "bin_status", apiTestDeref(api.machineBinStatus(machineID)), "stale")
	})

	t.Run("a heartbeat that fingerprints only one of the embedded pair stays unknown rather than current", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.binHashes = map[string]string{"ocwarden": "aaaa1111", "ocagent": "bbbb2222"}
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"binaries":{"ocwarden":"aaaa1111"}}`)

		apiWantValue(t, "bin_status", apiTestDeref(api.machineBinStatus(machineID)), nil)
	})

	t.Run("an empty binaries report and a build carrying no embedded pair are both unknown", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.binHashes = map[string]string{"ocwarden": "aaaa1111", "ocagent": "bbbb2222"}
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"binaries":{}}`)

		apiWantValue(t, "an empty report proves nothing",
			apiTestDeref(api.machineBinStatus(machineID)), nil)

		apiTestIngest(t, h, credential, `{"binaries":{"ocwarden":"aaaa1111","ocagent":"bbbb2222"}}`)
		api.binHashes = map[string]string{}

		apiWantValue(t, "nothing to compare against",
			apiTestDeref(api.machineBinStatus(machineID)), nil)
	})
}

// apiTestMachineCredential onboards a machine and keeps BOTH halves of the
// answer: the id the roster carries and the credential its warden heartbeats
// with (apiTestOnboardMachine drops the second).
func apiTestMachineCredential(t *testing.T, h http.Handler, owner, displayName string) (string, string) {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"`+displayName+`"}`)
	if status != 200 {
		t.Fatalf("onboard machine: %d %v", status, data)
	}
	id, _ := data["machine_id"].(string)
	credential, _ := data["token"].(string)
	if id == "" || credential == "" {
		t.Fatalf("onboard machine must mint an id and a credential: %v", data)
	}
	return id, credential
}

// apiTestIngest posts one warden heartbeat through the real ingest endpoint,
// so what lands in the telemetry store went through the same validation and
// partial merge a live warden's report does.
func apiTestIngest(t *testing.T, h http.Handler, credential, body string) {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", credential, body)
	if status != 200 {
		t.Fatalf("ingest telemetry %s: %d %v", body, status, data)
	}
}

// apiTestDeref renders an optional wire column the way the JSON encoder does,
// so a nil pointer and a reported value are compared on the same footing.
func apiTestDeref(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// apiTestDerefBool is apiTestDeref for the boolean columns.
func apiTestDerefBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

// apiTestMachineRow reads ONE machine's whole row off GET /api/machines — the
// surface every projection in this file ultimately reaches.
func apiTestMachineRow(t *testing.T, h http.Handler, owner, machineID string) map[string]any {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
	if rec.Code != 200 {
		t.Fatalf("list machines: %d (%s)", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	for _, row := range rows {
		if row["machine_id"] == machineID {
			return row
		}
	}
	t.Fatalf("machine %q is not in the roster: %v", machineID, rows)
	return nil
}

// apiTestMachineRowOf is an onboarded machine's row with every column at the
// value a warden that reported nothing leaves it, overlaid with what this
// scenario's heartbeat is expected to have moved.
func apiTestMachineRowOf(machineID, displayName string, over map[string]any) map[string]any {
	row := map[string]any{
		"machine_id":           machineID,
		"display_name":         displayName,
		"online":               false,
		"is_self":              false,
		"bin_status":           nil,
		"claude_version":       nil,
		"claude_cred_source":   nil,
		"claude_sub_readable":  nil,
		"runtime_capabilities": map[string]any{},
		"warden_shape":         nil,
		"cutover_effect":       nil,
		"token_key_id":         nil,
		"token_key_current":    nil,
	}
	for key, value := range over {
		row[key] = value
	}
	return row
}

func TestValidWardenShape(t *testing.T) {
	cases := []struct {
		shape string
		want  bool
	}{
		{"anchor", true},
		{"legacy", true},
		{"unknown", true},
		{"", false},
		{"Anchor", false},
		{"ANCHOR", false},
		{" anchor", false},
		{"anchor ", false},
		{"modern", false},
		{"effective", false},
		{"null", false},
	}
	for _, c := range cases {
		if got := ValidWardenShape(c.shape); got != c.want {
			t.Fatalf("ValidWardenShape(%q): want %v, got %v", c.shape, c.want, got)
		}
	}
}

func TestMachineWardenShape(t *testing.T) {
	t.Run("a warden that never reported a shape reads nil, which the wire keeps apart from a reported unknown", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiWantValue(t, "warden_shape", apiTestDeref(api.machineWardenShape(machineID)), nil)

		apiTestIngest(t, h, credential, `{"warden_shape":"unknown"}`)

		apiWantValue(t, "a REPORTED unknown", apiTestDeref(api.machineWardenShape(machineID)), "unknown")
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"warden_shape": "unknown",
			})))
	})

	t.Run("a reported shape is passed through untouched and reaches the wire", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"warden_shape":"anchor"}`)

		apiWantValue(t, "warden_shape", apiTestDeref(api.machineWardenShape(machineID)), "anchor")
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"warden_shape": "anchor",
			})))
	})

	t.Run("a later heartbeat carrying no shape leaves the reported one standing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"warden_shape":"legacy"}`)

		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiWantValue(t, "warden_shape", apiTestDeref(api.machineWardenShape(machineID)), "legacy")
	})

	t.Run("a shape outside the closed set never reaches the store because ingest refuses the whole report", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"warden_shape":"anchor"}`)

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", credential,
			`{"warden_shape":"modern"}`)

		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "warden_shape must be 'anchor', 'legacy' or 'unknown'")
		apiWantValue(t, "warden_shape", apiTestDeref(api.machineWardenShape(machineID)), "anchor")
	})

	t.Run("a machine nothing ever reported for reads nil", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, _ := apiTestMachineCredential(t, h, owner, "Studio Mac")

		apiWantValue(t, "warden_shape", apiTestDeref(api.machineWardenShape(machineID)), nil)
		apiWantValue(t, "an id no machine carries",
			apiTestDeref(api.machineWardenShape("m-never-onboarded")), nil)
	})
}

func TestValidCutoverEffect(t *testing.T) {
	cases := []struct {
		effect string
		want   bool
	}{
		{"effective", true},
		{"not_effective", true},
		{"unproven", true},
		{"", false},
		{"Effective", false},
		{"EFFECTIVE", false},
		{"not-effective", false},
		{"noteffective", false},
		{"unknown", false},
		{"anchor", false},
	}
	for _, c := range cases {
		if got := ValidCutoverEffect(c.effect); got != c.want {
			t.Fatalf("ValidCutoverEffect(%q): want %v, got %v", c.effect, c.want, got)
		}
	}
}

func TestMachineCutoverEffect(t *testing.T) {
	t.Run("a warden that never reported a verdict reads nil, which the wire keeps apart from a reported unproven", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiWantValue(t, "cutover_effect", apiTestDeref(api.machineCutoverEffect(machineID)), nil)

		apiTestIngest(t, h, credential, `{"cutover_effect":"unproven"}`)

		apiWantValue(t, "a REPORTED unproven",
			apiTestDeref(api.machineCutoverEffect(machineID)), "unproven")
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"cutover_effect": "unproven",
			})))
	})

	t.Run("a reported verdict is passed through untouched and reaches the wire", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cutover_effect":"not_effective"}`)

		apiWantValue(t, "cutover_effect",
			apiTestDeref(api.machineCutoverEffect(machineID)), "not_effective")
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"cutover_effect": "not_effective",
			})))
	})

	t.Run("a later heartbeat carrying no verdict leaves the reported one standing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cutover_effect":"effective"}`)

		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiWantValue(t, "cutover_effect",
			apiTestDeref(api.machineCutoverEffect(machineID)), "effective")
	})

	t.Run("a verdict outside the closed set never reaches the store because ingest refuses the whole report", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cutover_effect":"effective"}`)

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", credential,
			`{"cutover_effect":"partly"}`)

		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"cutover_effect must be 'effective', 'not_effective' or 'unproven'")
		apiWantValue(t, "cutover_effect",
			apiTestDeref(api.machineCutoverEffect(machineID)), "effective")
	})

	t.Run("a machine nothing ever reported for reads nil", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, _ := apiTestMachineCredential(t, h, owner, "Studio Mac")

		apiWantValue(t, "cutover_effect", apiTestDeref(api.machineCutoverEffect(machineID)), nil)
		apiWantValue(t, "an id no machine carries",
			apiTestDeref(api.machineCutoverEffect("m-never-onboarded")), nil)
	})
}

func TestMachineClaudeInfo(t *testing.T) {
	t.Run("a warden that reports no claude probe leaves all three columns unknown", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")

		apiTestWantClaudeInfo(t, api, machineID, nil, nil, nil)

		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiTestWantClaudeInfo(t, api, machineID, nil, nil, nil)

		apiTestIngest(t, h, credential, `{"claude":{}}`)

		apiTestWantClaudeInfo(t, api, machineID, nil, nil, nil)
	})

	t.Run("a probe reporting both presence bools synthesizes each of the four sources", func(t *testing.T) {
		cases := []struct {
			probe string
			want  any
		}{
			{`{"cred_file":true,"keychain":true}`, "both"},
			{`{"cred_file":true,"keychain":false}`, "file"},
			{`{"cred_file":false,"keychain":true}`, "keychain"},
			{`{"cred_file":false,"keychain":false}`, "none"},
		}
		for _, c := range cases {
			api, h, _, owner := newAPITestServer(t)
			machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
			apiTestIngest(t, h, credential, `{"claude":`+c.probe+`}`)

			apiTestWantClaudeInfo(t, api, machineID, nil, c.want, nil)
		}
	})

	t.Run("a lone true identifies its source while a lone false proves nothing", func(t *testing.T) {
		cases := []struct {
			probe string
			want  any
		}{
			{`{"cred_file":true}`, "file"},
			{`{"keychain":true}`, "keychain"},
			{`{"cred_file":false}`, nil},
			{`{"keychain":false}`, nil},
		}
		for _, c := range cases {
			api, h, _, owner := newAPITestServer(t)
			machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
			apiTestIngest(t, h, credential, `{"claude":`+c.probe+`}`)

			apiTestWantClaudeInfo(t, api, machineID, nil, c.want, nil)
		}
	})

	t.Run("a full probe reaches all three columns and the machine row that serves them", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential,
			`{"claude":{"version":"1.2.3","cred_file":true,"keychain":false,"sub_readable":false}}`)

		apiTestWantClaudeInfo(t, api, machineID, "1.2.3", "file", false)
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"claude_version":      "1.2.3",
				"claude_cred_source":  "file",
				"claude_sub_readable": false,
			})))
	})

	t.Run("an empty version string is unreported rather than a reported blank", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"claude":{"version":"","sub_readable":true}}`)

		apiTestWantClaudeInfo(t, api, machineID, nil, nil, true)
	})

	t.Run("a later heartbeat carrying no claude block leaves the probed columns standing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential,
			`{"claude":{"version":"1.2.3","cred_file":true,"keychain":true,"sub_readable":true}}`)

		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiTestWantClaudeInfo(t, api, machineID, "1.2.3", "both", true)
	})

	t.Run("a machine no telemetry was ever ingested for leaves all three unknown", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		apiTestWantClaudeInfo(t, api, "m-never-onboarded", nil, nil, nil)
	})
}

// apiTestWantClaudeInfo compares all three claude columns at once — they are
// one answer, and a test that reads only the one it is about cannot see a
// heartbeat that moved a neighbouring column.
func apiTestWantClaudeInfo(t *testing.T, api *apiServer, machineID string, version, credSource, subReadable any) {
	t.Helper()
	gotVersion, gotCredSource, gotSubReadable := api.machineClaudeInfo(machineID)
	apiWantValue(t, "claude info", any(map[string]any{
		"version":      apiTestDeref(gotVersion),
		"cred_source":  apiTestDeref(gotCredSource),
		"sub_readable": apiTestDerefBool(gotSubReadable),
	}), any(map[string]any{
		"version":      version,
		"cred_source":  credSource,
		"sub_readable": subReadable,
	}))
}

func TestMachineRuntimeCapabilities(t *testing.T) {
	t.Run("a machine that never heartbeated answers an empty map rather than a nil one", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, _ := apiTestMachineCredential(t, h, owner, "Studio Mac")

		got := api.machineRuntimeCapabilities(machineID)

		if got == nil {
			t.Fatalf("want an empty map, got nil")
		}
		apiTestWantEqual(t, "capabilities", got, map[string]RuntimeCapabilityDTO{})
	})

	t.Run("both probed runtimes are projected whole and reach the machine row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"runtimes":{`+
			`"claude":{"installed":true,"logged_in":true,"version":"1.2.3"},`+
			`"codex":{"installed":true,"logged_in":false,"version":"0.9"}}}`)

		apiTestWantEqual(t, "capabilities", api.machineRuntimeCapabilities(machineID),
			map[string]RuntimeCapabilityDTO{
				"claude": {Installed: apiTestBoolPtr(true), LoggedIn: apiTestBoolPtr(true),
					Version: apiTestStringPtr("1.2.3")},
				"codex": {Installed: apiTestBoolPtr(true), LoggedIn: apiTestBoolPtr(false),
					Version: apiTestStringPtr("0.9")},
			})
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"runtime_capabilities": map[string]any{
					"claude": map[string]any{"installed": true, "logged_in": true, "version": "1.2.3"},
					"codex":  map[string]any{"installed": true, "logged_in": false, "version": "0.9"},
				},
			})))
	})

	t.Run("a probe that reports a runtime with no fields at all is carried as an all-unknown capability", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"runtimes":{"codex":{}}}`)

		apiTestWantEqual(t, "capabilities", api.machineRuntimeCapabilities(machineID),
			map[string]RuntimeCapabilityDTO{"codex": {}})
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"runtime_capabilities": map[string]any{"codex": map[string]any{}},
			})))
	})

	t.Run("a null logged_in is unknown rather than false", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"runtimes":{"codex":{"installed":true,"logged_in":null,"version":null}}}`)

		apiTestWantEqual(t, "capabilities", api.machineRuntimeCapabilities(machineID),
			map[string]RuntimeCapabilityDTO{"codex": {Installed: apiTestBoolPtr(true)}})
	})

	t.Run("a runtime name outside the closed set is refused at ingest and skipped by the projection", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")

		status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", credential,
			`{"runtimes":{"gpt":{"installed":true}}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "runtimes keys must be 'claude' or 'codex'")
		apiTestWantEqual(t, "capabilities", api.machineRuntimeCapabilities(machineID),
			map[string]RuntimeCapabilityDTO{})

		api.telemetry.Set(machineID, map[string]any{"runtimes": map[string]any{
			"gpt":    map[string]any{"installed": true},
			"codex":  "not an object",
			"claude": map[string]any{"installed": true},
		}})

		apiTestWantEqual(t, "capabilities", api.machineRuntimeCapabilities(machineID),
			map[string]RuntimeCapabilityDTO{"claude": {Installed: apiTestBoolPtr(true)}})
	})

	t.Run("a later heartbeat carrying no runtimes leaves the probed map standing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"runtimes":{"claude":{"installed":true}}}`)

		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiTestWantEqual(t, "capabilities", api.machineRuntimeCapabilities(machineID),
			map[string]RuntimeCapabilityDTO{"claude": {Installed: apiTestBoolPtr(true)}})
	})
}

func apiTestBoolPtr(v bool) *bool { return &v }

func apiTestStringPtr(v string) *string { return &v }

func TestMachineSupportsRuntime(t *testing.T) {
	t.Run("a warden that probed nothing is a claude warden by construction and never a codex one", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiTestWantSupports(t, api, machineID, map[string]any{
			"claude": true, "codex": false, "": true, "gpt": false,
		})
	})

	t.Run("a probed map that never mentions a runtime is that runtime's absence, not its unknown", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"runtimes":{"claude":{"installed":true,"logged_in":true}}}`)

		apiTestWantSupports(t, api, machineID, map[string]any{
			"claude": true, "codex": false, "": true, "gpt": false,
		})
	})

	t.Run("claude stays permitted even when its own probe says not installed and not logged in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential,
			`{"runtimes":{"claude":{"installed":false,"logged_in":false},"codex":{"installed":true}}}`)

		apiTestWantSupports(t, api, machineID, map[string]any{"claude": true, "codex": true})
	})

	t.Run("codex needs an installed probe and refuses a reported logged-out one", func(t *testing.T) {
		cases := []struct {
			probe string
			want  bool
		}{
			{`{"installed":true,"logged_in":true}`, true},
			{`{"installed":true}`, true},
			{`{"installed":true,"logged_in":null}`, true},
			{`{"installed":true,"logged_in":false}`, false},
			{`{"installed":false,"logged_in":true}`, false},
			{`{"logged_in":true}`, false},
			{`{}`, false},
		}
		for _, c := range cases {
			api, h, _, owner := newAPITestServer(t)
			machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
			apiTestIngest(t, h, credential, `{"runtimes":{"codex":`+c.probe+`}}`)

			if got := api.machineSupportsRuntime(machineID, "codex"); got != c.want {
				t.Fatalf("codex probe %s: want %v, got %v", c.probe, c.want, got)
			}
		}
	})

	t.Run("a runtime name is trimmed before it is looked up, and a blank one asks about claude", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"runtimes":{"codex":{"installed":true}}}`)

		apiTestWantSupports(t, api, machineID, map[string]any{
			"codex": true, " codex ": true, "claude": false, "": false, "   ": false,
		})
	})
}

// apiTestWantSupports asks the placement gate about several runtimes at once,
// so the answer for one is always read beside the answers for its neighbours.
func apiTestWantSupports(t *testing.T, api *apiServer, machineID string, want map[string]any) {
	t.Helper()
	got := map[string]any{}
	for runtime := range want {
		got[runtime] = api.machineSupportsRuntime(machineID, runtime)
	}
	apiWantValue(t, "supports", any(got), any(want))
}

func TestMachineTokenKey(t *testing.T) {
	t.Run("a machine no credential of has ever been verified answers neither an id nor a verdict", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiTestIngest(t, h, credential, `{"cost":2.5}`)

		apiTestWantTokenKey(t, api, apiTestMemberRow(t, d, machineID), nil, nil)
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", nil)))
	})

	t.Run("a machine that opened its downstream is recorded on the key that verified it, and that key is the current one", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		apiEventsStream(t, h, credential, "")

		apiTestWantTokenKey(t, api, apiTestMemberRow(t, d, machineID), "k-legacy", true)
		apiWantValue(t, "row", any(apiTestMachineRow(t, h, owner, machineID)),
			any(apiTestMachineRowOf(machineID, "Studio Mac", map[string]any{
				"online":            true,
				"token_key_id":      "k-legacy",
				"token_key_current": true,
			})))
	})

	t.Run("a rotation moves the verdict on the very next read without moving the recorded id", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID, credential := apiTestMachineCredential(t, h, owner, "Studio Mac")
		stream := apiEventsStream(t, h, credential, "")
		machine := apiTestMemberRow(t, d, machineID)
		apiTestWantTokenKey(t, api, machine, "k-legacy", true)

		if status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", owner, `{}`); status != 200 {
			t.Fatalf("rotate: %d %v", status, data)
		}

		apiTestWantTokenKey(t, api, machine, "k-legacy", false)
		stream.stop()
	})

	t.Run("a station with no signing key at all reports the recorded id as not current", func(t *testing.T) {
		api, _, _, _ := newAPITestStackWithoutSigningSecret(t)

		apiTestWantTokenKey(t, api, Member{ID: "m-studio", TokenKeyID: "k-legacy"}, "k-legacy", false)
	})
}

// apiTestWantTokenKey compares both halves of the T-80 answer at once: they are
// nil together or present together, and reading one alone cannot see that.
func apiTestWantTokenKey(t *testing.T, api *apiServer, m Member, wantID, wantCurrent any) {
	t.Helper()
	id, current := api.machineTokenKey(m)
	apiWantValue(t, "token key", any(map[string]any{
		"id": apiTestDeref(id), "current": apiTestDerefBool(current),
	}), any(map[string]any{"id": wantID, "current": wantCurrent}))
}

func TestHandleListMachinesApiMachinesGet(t *testing.T) {
	t.Run("the out-of-box roster answers the server-local machine alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestServerSelfMachineRow(false)})
		dashboard.wantFrames()
	})

	t.Run("an onboarded machine is listed after the server-local row under its overlay name", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiJSON(t, h, "PATCH", "/api/machines/"+machineID, owner, `{"display_name":"Loft"}`)

		rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{
			apiTestServerSelfMachineRow(false),
			map[string]any{
				"machine_id":           machineID,
				"display_name":         "Loft",
				"online":               false,
				"is_self":              false,
				"bin_status":           nil,
				"claude_version":       nil,
				"claude_cred_source":   nil,
				"claude_sub_readable":  nil,
				"runtime_capabilities": map[string]any{},
				"warden_shape":         nil,
				"cutover_effect":       nil,
				"token_key_id":         nil,
				"token_key_current":    nil,
			},
		})
	})

	t.Run("a machine holding a live connection is listed online", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestListen(t, api, ServerSelfHost)

		rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestServerSelfMachineRow(true)})
	})

	t.Run("an ordinary agent identity is served the same roster because this row's floor is machine", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		rec := apiRequest(t, h, "GET", "/api/machines", agent, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestServerSelfMachineRow(false)})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/machines", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

// apiTestServerSelfMachineRow is the out-of-box seed's own machine row as
// GET /api/machines renders it — every column a warden that has never
// heartbeated leaves unknown.
func apiTestServerSelfMachineRow(online bool) map[string]any {
	return map[string]any{
		"machine_id":           "m-server-self",
		"display_name":         "伺服器這一台",
		"online":               online,
		"is_self":              true,
		"bin_status":           nil,
		"claude_version":       nil,
		"claude_cred_source":   nil,
		"claude_sub_readable":  nil,
		"runtime_capabilities": map[string]any{},
		"warden_shape":         nil,
		"cutover_effect":       nil,
		"token_key_id":         nil,
		"token_key_current":    nil,
	}
}

func TestHandleOnboardMachineApiMachinesPost(t *testing.T) {
	t.Run("onboarding a machine answers the minted credential and installer one-liner and fans the machine's member delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"Studio Mac"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":        apiAnyString,
			"machine_id":       apiAnyString,
			"token":            apiAnyString,
			"expires_in":       2592000,
			"boot_command":     apiAnyString,
			"claim_code":       apiAnyString,
			"claim_expires_in": 600,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":            apiAnyString,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a display name that is only whitespace answers 422 and onboards nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name is required")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines", agent, `{"display_name":"Studio Mac"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines", "", `{"display_name":"Studio Mac"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestClearResidualUninstall(t *testing.T) {
	t.Run("a residual uninstall intent is zeroed on the caller's copy, in the roster and on the wire", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		apiTestWantDesiredState(t, d, machineID, "uninstall")
		machine := apiTestMemberRow(t, d, machineID)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		if err := api.clearResidualUninstall(&machine, "owner"); err != nil {
			t.Fatalf("clearResidualUninstall: %v", err)
		}

		want := machine
		want.DesiredState = DesiredStateOffline
		apiTestWantEqual(t, "the caller's copy", machine, want)
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, machineID), want)
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a machine carrying no residue is left alone, writes nothing and fans nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		before := apiTestMemberRow(t, d, machineID)
		dashboard := apiTestListen(t, api, "")

		machine := before
		if err := api.clearResidualUninstall(&machine, "owner"); err != nil {
			t.Fatalf("clearResidualUninstall: %v", err)
		}

		apiTestWantEqual(t, "the caller's copy", machine, before)
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, machineID), before)
		dashboard.wantFrames()
	})

	t.Run("an intent to be online is not residue and survives untouched", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		online := apiTestMemberRow(t, d, machineID)
		online.DesiredState = DesiredStateOnline
		if err := api.putMember(online, "owner"); err != nil {
			t.Fatalf("putMember: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		machine := online
		if err := api.clearResidualUninstall(&machine, "owner"); err != nil {
			t.Fatalf("clearResidualUninstall: %v", err)
		}

		apiTestWantEqual(t, "the caller's copy", machine, online)
		apiTestWantEqual(t, "stored row", apiTestMemberRow(t, d, machineID), online)
		dashboard.wantFrames()
	})
}

func TestHandleMachineBootCommandApiMachinesMachineIdBootCommandGet(t *testing.T) {
	t.Run("a machine re-fetching its one-liner is handed a claim code and a freshly minted credential", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"machine_id":       machineID,
			"boot_command":     apiAnyString,
			"token":            apiAnyString,
			"expires_in":       2592000,
			"claim_code":       apiAnyString,
			"claim_expires_in": 600,
		})
		claimCode, _ := data["claim_code"].(string)
		wantCommand := "curl -fsSL 'https://example.com/install.sh?code=" + claimCode + "' | bash"
		if got, _ := data["boot_command"].(string); got != wantCommand {
			t.Fatalf("boot_command: want %q, got %q", wantCommand, got)
		}
		dashboard.wantFrames()
	})

	t.Run("a residual uninstall intent is zeroed before the fresh one-liner is handed out", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		apiTestWantDesiredState(t, d, machineID, "offline")
	})

	t.Run("a machine id nothing carries answers 404 naming it and mints nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/nope/boot-command", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/machines/m-server-self/boot-command", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

// apiTestWantDesiredState asserts the durable machine intent the roster now
// carries — the half of a machine-lifecycle write that never reaches the wire.
func apiTestWantDesiredState(t *testing.T, d *DAL, machineID, want string) {
	t.Helper()
	m, err := d.GetMember(machineID)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", machineID, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%q): no such row", machineID)
	}
	if m.DesiredState != want {
		t.Fatalf("desired_state: want %q, got %q", want, m.DesiredState)
	}
}

// apiTestWantRosterStatus is apiTestWantDesiredState for the other half a
// teardown would move: whether the row is still on the roster at all.
func apiTestWantRosterStatus(t *testing.T, d *DAL, machineID, want string) {
	t.Helper()
	m, err := d.GetMember(machineID)
	if err != nil {
		t.Fatalf("GetMember(%q): %v", machineID, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%q): no such row", machineID)
	}
	if m.RosterStatus != want {
		t.Fatalf("roster_status: want %q, got %q", want, m.RosterStatus)
	}
}

func TestHandleClaimMachineTokenApiMachinesClaimPost(t *testing.T) {
	t.Run("a fresh claim code is exchanged for the machine's credential by a caller carrying no identity", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		_, onboarded := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		code, _ := onboarded["claim_code"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"`+code+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"expires_in": 2592000,
			"machine_id": machineID,
		})
		dashboard.wantFrames()
	})

	t.Run("the same code a second time answers the flat refusal", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		_, onboarded := apiJSON(t, h, "GET", "/api/machines/"+machineID+"/boot-command", owner, "")
		code, _ := onboarded["claim_code"].(string)
		apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"`+code+`"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"`+code+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", apiTestClaimDeniedMsg)
		dashboard.wantFrames()
	})

	t.Run("a code nothing ever minted is refused in the same words as a used one", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{"code":"nothing-was-ever-minted-here"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", apiTestClaimDeniedMsg)
	})

	t.Run("a body naming no code answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: code")
	})

	t.Run("a body that is not JSON answers 422 without reaching the redemption", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/claim", "", `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})
}

// apiTestClaimDeniedMsg is the single sentence every failed redemption gets —
// unknown, expired and already-used codes are indistinguishable on the wire.
const apiTestClaimDeniedMsg = "claim code is invalid, expired, or already used — " +
	"fetch a fresh boot command from the cockpit"

func TestHandleRenewMachineCredentialApiMachinesRenewCredentialPost(t *testing.T) {
	t.Run("a warden trades its own credential for a fresh one bound to the same machine", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		status, onboarded := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"Studio Mac"}`)
		if status != 200 {
			t.Fatalf("onboard: %d %v", status, onboarded)
		}
		machineID, _ := onboarded["machine_id"].(string)
		held, _ := onboarded["token"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", held, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"expires_in": 2592000,
			"machine_id": machineID,
		})
		renewed, _ := data["token"].(string)
		if again, body := apiJSON(t, h, "GET", "/api/machines", renewed, ""); again != 200 {
			t.Fatalf("the renewed credential must open the machine roster, got %d (%v)", again, body)
		}
		dashboard.wantFrames()
	})

	t.Run("an ordinary agent identity answers 403 because it is not a machine", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", apiTestRenewNotAWardenMsg)
	})

	t.Run("the owner answers the same 403 because this row acts on the caller alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", owner, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", apiTestRenewNotAWardenMsg)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/renew-credential", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

const apiTestRenewNotAWardenMsg = "only a machine credential can be renewed here; " +
	"this endpoint acts on the caller and takes no target"

func TestHandleMachineCredentialPolicyApiMachinesCredentialPolicyGet(t *testing.T) {
	t.Run("the station answers its shipped machine-credential lifetime", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lifetime_secs": 2592000})
		dashboard.wantFrames()
	})

	t.Run("a lifetime the owner lowered is what the next poll reads", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"warden_credential_lifetime_secs":86400}`); status != 200 {
			t.Fatalf("settings patch: %d %v", status, data)
		}

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lifetime_secs": 86400})
	})

	t.Run("an ordinary agent identity reads the same number", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lifetime_secs": 2592000})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/machines/credential-policy", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestOcwardenChildEnv(t *testing.T) {
	t.Run("the allowlisted five survive in the order the parent carried them and everything else is dropped", func(t *testing.T) {
		got := ocwardenChildEnv([]string{
			"OC_ID=m-impostor",
			"HOME=/Users/eva",
			"OC_NAMESPACE=stray",
			"PATH=/usr/bin:/bin",
			"OC_BASE=https://stray.example",
			"OC_CLAUDE_BIN=/opt/claude",
			"OC_TOKEN=stray-token",
			"OC_CODEX_BIN=/opt/codex",
			"OC_AGENT_BIN=/opt/stray",
			"OC_CLAUDE_CRED_CHECK=0",
			"WARDEN_INSTALL_DRYRUN=1",
			"LANG=en_US.UTF-8",
		})

		apiTestWantEqual(t, "child env", got, []string{
			"HOME=/Users/eva",
			"PATH=/usr/bin:/bin",
			"OC_CLAUDE_BIN=/opt/claude",
			"OC_CODEX_BIN=/opt/codex",
			"OC_CLAUDE_CRED_CHECK=0",
		})
	})

	t.Run("a key set to empty is relayed as set-to-empty while a parent carrying none answers an empty slice rather than nil", func(t *testing.T) {
		apiTestWantEqual(t, "an empty value", ocwardenChildEnv([]string{"HOME="}), []string{"HOME="})

		got := ocwardenChildEnv([]string{"LANG=C"})

		if got == nil {
			t.Fatalf("want an empty slice, got nil")
		}
		apiTestWantEqual(t, "nothing allowlisted", got, []string{})
	})

	t.Run("an entry carrying no name is dropped rather than relayed", func(t *testing.T) {
		apiTestWantEqual(t, "child env",
			ocwardenChildEnv([]string{"HOME", "=orphan", "HOME=/root"}), []string{"HOME=/root"})
	})

	t.Run("a repeated allowlisted key is relayed as many times as the parent carried it", func(t *testing.T) {
		apiTestWantEqual(t, "child env",
			ocwardenChildEnv([]string{"PATH=/a", "PATH=/b"}), []string{"PATH=/a", "PATH=/b"})
	})

	t.Run("an empty parent env answers an empty slice", func(t *testing.T) {
		got := ocwardenChildEnv(nil)

		if got == nil {
			t.Fatalf("want an empty slice, got nil")
		}
		apiTestWantEqual(t, "child env", got, []string{})
	})
}

func TestExecOcwarden(t *testing.T) {
	t.Run("the child's argv, its merged output and the code it exited on all come back", func(t *testing.T) {
		bin := apiTestOcwardenStub(t, "#!/bin/sh\necho \"argv:$*\"\necho \"to stderr\" >&2\nexit \"$1\"\n")

		exitCode, log, timedOut := execOcwarden(bin, []string{"7", "--force"}, []string{"PATH=/usr/bin:/bin"})

		if exitCode != 7 || timedOut {
			t.Fatalf("want (7, false), got (%d, %v)", exitCode, timedOut)
		}
		if log != "argv:7 --force\nto stderr\n" {
			t.Fatalf("log: %q", log)
		}
	})

	t.Run("a child that exits zero answers zero and its output", func(t *testing.T) {
		bin := apiTestOcwardenStub(t, "#!/bin/sh\necho installed\n")

		exitCode, log, timedOut := execOcwarden(bin, []string{"install", "--force"}, nil)

		if exitCode != 0 || log != "installed\n" || timedOut {
			t.Fatalf("got (%d, %q, %v)", exitCode, log, timedOut)
		}
	})

	t.Run("the child reads the env it was handed and none of the test process's own", func(t *testing.T) {
		t.Setenv("OC_ID", "m-impostor")
		t.Setenv("OC_CLAUDE_BIN", "/opt/claude")
		bin := apiTestOcwardenStub(t,
			"#!/bin/sh\necho \"base=$OC_BASE id=${OC_ID-unset} claude=${OC_CLAUDE_BIN-unset}\"\n")

		exitCode, log, timedOut := execOcwarden(bin, []string{"teardown"},
			[]string{"PATH=/usr/bin:/bin", "OC_BASE=https://example.com"})

		if exitCode != 0 || timedOut {
			t.Fatalf("want (0, false), got (%d, %v): %q", exitCode, timedOut, log)
		}
		if log != "base=https://example.com id=unset claude=unset\n" {
			t.Fatalf("child env: %q", log)
		}
	})

	t.Run("a path with no binary behind it answers -1 and the failure itself as the log", func(t *testing.T) {
		missing := t.TempDir() + "/ocwarden"

		exitCode, log, timedOut := execOcwarden(missing, []string{"install"}, nil)

		if exitCode != -1 || timedOut {
			t.Fatalf("want (-1, false), got (%d, %v)", exitCode, timedOut)
		}
		if log != "fork/exec "+missing+": no such file or directory" {
			t.Fatalf("log: %q", log)
		}
	})

	t.Run("a file that is not executable answers -1 and names the refusal", func(t *testing.T) {
		path := t.TempDir() + "/ocwarden"
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		exitCode, log, timedOut := execOcwarden(path, []string{"install"}, nil)

		if exitCode != -1 || timedOut {
			t.Fatalf("want (-1, false), got (%d, %v)", exitCode, timedOut)
		}
		if log != "fork/exec "+path+": permission denied" {
			t.Fatalf("log: %q", log)
		}
	})
}

// apiTestOcwardenStub writes an executable stand-in for the ocwarden binary
// OUTSIDE the package tree, so the runner is exercised against a real child
// process without any of this repo's own artifacts being involved.
func apiTestOcwardenStub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ocwarden")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func TestResolveOcwardenBinary(t *testing.T) {
	t.Run("an embedded warden is materialized into the binary cache", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cache := t.TempDir()
		api.binCacheDir = cache
		api.ocwardenFS = fstest.MapFS{
			"ocwarden":  &fstest.MapFile{Data: []byte("warden bytes")},
			"officraft": &fstest.MapFile{Data: []byte("anchor bytes")},
		}
		rec := httptest.NewRecorder()

		path, ok := api.resolveOcwardenBinary(rec)

		if !ok {
			t.Fatalf("want ok, got %d (%s)", rec.Code, rec.Body.String())
		}
		if path != cache+"/ocwarden" {
			t.Fatalf("path: %q", path)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("nothing may be written on success, got %s", rec.Body.String())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data) != "warden bytes" {
			t.Fatalf("materialized: %q", data)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode: %v", info.Mode().Perm())
		}
	})

	t.Run("a build carrying no embedded warden answers 503", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.binCacheDir = t.TempDir()
		api.ocwardenFS = fstest.MapFS{}
		rec := httptest.NewRecorder()

		path, ok := api.resolveOcwardenBinary(rec)

		if ok || path != "" {
			t.Fatalf("want a refusal, got %q", path)
		}
		if rec.Code != 503 {
			t.Fatalf("want 503, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, map[string]any{
			"error": map[string]any{
				"code":    "service_unavailable",
				"message": "ocwarden binary is not available (no embedded copy in this server build): open ocwarden: file does not exist",
			},
		})
	})
}

func TestResolveOcwardenBinaryFrom(t *testing.T) {
	t.Run("the warden and its anchor are both materialized executable and the warden's path is answered", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cache := t.TempDir()
		api.binCacheDir = cache

		path, err := api.resolveOcwardenBinaryFrom(fstest.MapFS{
			"ocwarden":  &fstest.MapFile{Data: []byte("warden bytes")},
			"officraft": &fstest.MapFile{Data: []byte("anchor bytes")},
			"ocagent":   &fstest.MapFile{Data: []byte("agent bytes")},
		})

		if err != nil {
			t.Fatalf("resolveOcwardenBinaryFrom: %v", err)
		}
		if path != cache+"/ocwarden" {
			t.Fatalf("path: %q", path)
		}
		apiTestWantEqual(t, "materialized", apiTestCacheFiles(t, cache),
			[]string{"ocwarden", "officraft"})
		apiTestWantExecutable(t, cache+"/ocwarden", "warden bytes")
		apiTestWantExecutable(t, cache+"/officraft", "anchor bytes")
	})

	t.Run("an embed carrying no warden answers the read failure and materializes nothing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cache := t.TempDir()
		api.binCacheDir = cache

		path, err := api.resolveOcwardenBinaryFrom(fstest.MapFS{
			"officraft": &fstest.MapFile{Data: []byte("anchor bytes")},
		})

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if err == nil || err.Error() != "open ocwarden: file does not exist" {
			t.Fatalf("err: %v", err)
		}
		apiTestWantEqual(t, "materialized", apiTestCacheFiles(t, cache), []string{})
	})

	t.Run("an embed carrying no anchor answers the read failure and materializes nothing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cache := t.TempDir()
		api.binCacheDir = cache

		path, err := api.resolveOcwardenBinaryFrom(fstest.MapFS{
			"ocwarden": &fstest.MapFile{Data: []byte("warden bytes")},
		})

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if err == nil || err.Error() != "open officraft: file does not exist" {
			t.Fatalf("err: %v", err)
		}
		apiTestWantEqual(t, "materialized", apiTestCacheFiles(t, cache), []string{})
	})

	t.Run("a server with no binary cache configured refuses before writing anything", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.binCacheDir = ""

		path, err := api.resolveOcwardenBinaryFrom(fstest.MapFS{
			"ocwarden":  &fstest.MapFile{Data: []byte("warden bytes")},
			"officraft": &fstest.MapFile{Data: []byte("anchor bytes")},
		})

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if err == nil || err.Error() != "no binary cache directory configured" {
			t.Fatalf("err: %v", err)
		}
	})

	t.Run("a second resolve over changed bytes rewrites the cached copy", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cache := t.TempDir()
		api.binCacheDir = cache
		if _, err := api.resolveOcwardenBinaryFrom(fstest.MapFS{
			"ocwarden":  &fstest.MapFile{Data: []byte("warden bytes")},
			"officraft": &fstest.MapFile{Data: []byte("anchor bytes")},
		}); err != nil {
			t.Fatalf("first resolve: %v", err)
		}

		path, err := api.resolveOcwardenBinaryFrom(fstest.MapFS{
			"ocwarden":  &fstest.MapFile{Data: []byte("newer warden bytes")},
			"officraft": &fstest.MapFile{Data: []byte("newer anchor bytes")},
		})

		if err != nil {
			t.Fatalf("second resolve: %v", err)
		}
		if path != cache+"/ocwarden" {
			t.Fatalf("path: %q", path)
		}
		apiTestWantEqual(t, "materialized", apiTestCacheFiles(t, cache),
			[]string{"ocwarden", "officraft"})
		apiTestWantExecutable(t, cache+"/ocwarden", "newer warden bytes")
		apiTestWantExecutable(t, cache+"/officraft", "newer anchor bytes")
	})
}

// apiTestCacheFiles is the WHOLE content of the per-instance binary cache, so
// a refusal that half-wrote it is visible rather than merely unasserted.
func apiTestCacheFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func apiTestWantExecutable(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s: want %q, got %q", path, want, data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("%s mode: %v", path, info.Mode().Perm())
	}
}

func TestBootstrapHereForeignTargetMsg(t *testing.T) {
	got := bootstrapHereForeignTargetMsg("m-studio")

	if got != "bootstrap-here only ever installs the warden running on THIS server "+
		"host — it carries no machine selector, so it cannot reach m-studio"+
		"; refusing rather than overwriting this host's warden with another "+
		"machine's identity. To install a different machine, fetch its own "+
		"one-liner with GET /api/machines/{member_id}/boot-command and run it "+
		"on that host." {
		t.Fatalf("refusal: %q", got)
	}
}

func TestBootstrapHereRefusal(t *testing.T) {
	t.Run("the server-local machine may proceed and is owed nothing", func(t *testing.T) {
		if got := bootstrapHereRefusal(ServerSelfHost); got != "" {
			t.Fatalf("refusal: %q", got)
		}
	})

	t.Run("any other machine is owed the foreign-target refusal naming it", func(t *testing.T) {
		for _, machineID := range []string{"m-studio", "mbp5", ""} {
			if got := bootstrapHereRefusal(machineID); got != bootstrapHereForeignTargetMsg(machineID) {
				t.Fatalf("refusal for %q: %q", machineID, got)
			}
		}
	})
}

func TestHandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(t *testing.T) {
	t.Run("the server-local machine clears a residual uninstall intent and then reports the missing embedded binary", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		apiTestListen(t, api, ServerSelfHost)
		apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/uninstall", owner, `{}`)
		apiTestWantDesiredState(t, d, ServerSelfHost, "uninstall")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/bootstrap-here", owner, `{}`)
		if status != 503 {
			t.Fatalf("want 503, got %d (%v)", status, data)
		}
		apiWantError(t, data, "service_unavailable",
			"ocwarden binary is not available (no embedded copy in this server build): open ocwarden: file does not exist")
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + ServerSelfHost,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            ServerSelfHost,
					"name":          "伺服器這一台",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		apiTestWantDesiredState(t, d, ServerSelfHost, "offline")
	})

	t.Run("a machine other than the server's own is refused and its uninstall intent survives untouched", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/bootstrap-here", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"bootstrap-here only ever installs the warden running on THIS server host — "+
				"it carries no machine selector, so it cannot reach "+machineID+
				"; refusing rather than overwriting this host's warden with another "+
				"machine's identity. To install a different machine, fetch its own "+
				"one-liner with GET /api/machines/{member_id}/boot-command and run it "+
				"on that host.")
		dashboard.wantFrames()
		apiTestWantDesiredState(t, d, machineID, "uninstall")
	})

	t.Run("a machine id nothing carries answers 404 naming it and installs nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/bootstrap-here", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/bootstrap-here", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/bootstrap-here", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestRunWardenInstallHere(t *testing.T) {
	t.Run("the child is run as install --force over the allowlisted parent env plus a credential that opens this machine's roster", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		runs := apiTestRecordOcwarden(t, 0, "warden installed\n", false)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")

		got, err := api.runWardenInstallHere(apiTestMemberRow(t, d, machineID),
			"/tmp/ocwarden", "https://example.com")

		if err != nil {
			t.Fatalf("runWardenInstallHere: %v", err)
		}
		apiTestWantEqual(t, "result", got, bootstrapResultDTO{
			MachineID: machineID, OK: true, ExitCode: 0, Log: "warden installed\n",
		})
		if len(*runs) != 1 {
			t.Fatalf("want one child run, got %d (%v)", len(*runs), *runs)
		}
		run := (*runs)[0]
		if run.binPath != "/tmp/ocwarden" {
			t.Fatalf("binPath: %q", run.binPath)
		}
		apiTestWantEqual(t, "argv", run.args, []string{"install", "--force"})
		credential, rest := apiTestSplitEnv(t, run.env, "OC_TOKEN")
		apiTestWantEqual(t, "child env", rest, []string{
			"HOME=/tmp/oc-home",
			"OC_BASE=https://example.com",
			"OC_CLAUDE_BIN=/opt/claude",
			"OC_CLAUDE_CRED_CHECK=0",
			"OC_CODEX_BIN=/opt/codex",
			"PATH=/usr/bin:/bin",
		})
		if status, data := apiJSON(t, h, "GET", "/api/machines", credential, ""); status != 200 {
			t.Fatalf("the relayed credential must open this machine's roster: %d %v", status, data)
		}
	})

	t.Run("a namespaced instance additionally relays its own namespace and nothing the parent env claimed", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		api.namespace = "bench"
		runs := apiTestRecordOcwarden(t, 0, "", false)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")

		if _, err := api.runWardenInstallHere(apiTestMemberRow(t, d, machineID),
			"/tmp/ocwarden", "https://example.com"); err != nil {
			t.Fatalf("runWardenInstallHere: %v", err)
		}

		_, rest := apiTestSplitEnv(t, (*runs)[0].env, "OC_TOKEN")
		apiTestWantEqual(t, "child env", rest, []string{
			"HOME=/tmp/oc-home",
			"OC_BASE=https://example.com",
			"OC_CLAUDE_BIN=/opt/claude",
			"OC_CLAUDE_CRED_CHECK=0",
			"OC_CODEX_BIN=/opt/codex",
			"OC_NAMESPACE=bench",
			"PATH=/usr/bin:/bin",
		})
	})

	t.Run("a non-zero exit is a result rather than an error", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		apiTestRecordOcwarden(t, 3, "launchctl: bootstrap failed\n", false)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")

		got, err := api.runWardenInstallHere(apiTestMemberRow(t, d, machineID),
			"/tmp/ocwarden", "https://example.com")

		if err != nil {
			t.Fatalf("runWardenInstallHere: %v", err)
		}
		apiTestWantEqual(t, "result", got, bootstrapResultDTO{
			MachineID: machineID, OK: false, ExitCode: 3, Log: "launchctl: bootstrap failed\n",
		})
	})

	t.Run("a timed-out install answers the no-changes-confirmed log instead of whatever the child had printed", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		apiTestRecordOcwarden(t, -1, "half of an install\n", true)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")

		got, err := api.runWardenInstallHere(apiTestMemberRow(t, d, machineID),
			"/tmp/ocwarden", "https://example.com")

		if err != nil {
			t.Fatalf("runWardenInstallHere: %v", err)
		}
		apiTestWantEqual(t, "result", got, bootstrapResultDTO{
			MachineID: machineID, OK: false, ExitCode: -1,
			Log: "ocwarden install timed out (exceeded 60s) — no changes confirmed",
		})
	})

	t.Run("a station that can mint nothing answers the mint failure and never reaches the child", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		runs := apiTestRecordOcwarden(t, 0, "", false)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		machine := apiTestMemberRow(t, d, machineID)
		api.keys = singleKeyring(nil)

		got, err := api.runWardenInstallHere(machine, "/tmp/ocwarden", "https://example.com")

		if err == nil {
			t.Fatalf("want a mint failure, got %#v", got)
		}
		apiTestWantEqual(t, "result", got, bootstrapResultDTO{})
		apiTestWantEqual(t, "child runs", *runs, []apiTestOcwardenRun{})
	})
}

// apiTestOcwardenRun is one recorded call through the runOcwarden seam — the
// only place the env a host-mutating verb would hand its child is observable.
type apiTestOcwardenRun struct {
	binPath string
	args    []string
	env     []string
}

// apiTestRecordOcwarden rebinds the runOcwarden seam for the length of one test
// and answers the slice the calls land in, plus the reply the fake child gives.
func apiTestRecordOcwarden(t *testing.T, exitCode int, log string, timedOut bool) *[]apiTestOcwardenRun {
	t.Helper()
	runs := []apiTestOcwardenRun{}
	previous := runOcwarden
	runOcwarden = func(binPath string, args []string, env []string) (int, string, bool) {
		runs = append(runs, apiTestOcwardenRun{binPath: binPath, args: args, env: env})
		return exitCode, log, timedOut
	}
	t.Cleanup(func() { runOcwarden = previous })
	return &runs
}

// apiTestArmOcwardenEnv puts the server process env into a state a test can
// name: every allowlisted key set, and beside them the strays the projection
// exists to drop (an identity claim, a namespace, a base and a credential).
func apiTestArmOcwardenEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "/tmp/oc-home")
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("OC_CLAUDE_BIN", "/opt/claude")
	t.Setenv("OC_CODEX_BIN", "/opt/codex")
	t.Setenv("OC_CLAUDE_CRED_CHECK", "0")
	t.Setenv("OC_ID", "m-impostor")
	t.Setenv("OC_NAMESPACE", "stray")
	t.Setenv("OC_BASE", "https://stray.example")
	t.Setenv("OC_TOKEN", "stray-token")
	t.Setenv("OC_AGENT_BIN", "/opt/stray")
	t.Setenv("WARDEN_INSTALL_DRYRUN", "1")
}

// apiTestAllowlistedChildEnv is what apiTestArmOcwardenEnv's process env
// projects down to, sorted the way apiTestSplitEnv answers.
func apiTestAllowlistedChildEnv() []string {
	return []string{
		"HOME=/tmp/oc-home",
		"OC_CLAUDE_BIN=/opt/claude",
		"OC_CLAUDE_CRED_CHECK=0",
		"OC_CODEX_BIN=/opt/codex",
		"PATH=/usr/bin:/bin",
	}
}

// apiTestSplitEnv lifts the one entry whose value cannot be written down — a
// freshly minted credential — out of a recorded child env, so that value can be
// asserted by USING it while everything else is compared whole. The rest comes
// back sorted, because the projection's order is the parent process's.
func apiTestSplitEnv(t *testing.T, env []string, key string) (string, []string) {
	t.Helper()
	value := ""
	found := 0
	rest := []string{}
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			value = strings.TrimPrefix(entry, key+"=")
			found++
			continue
		}
		rest = append(rest, entry)
	}
	if found != 1 {
		t.Fatalf("want exactly one %s entry, got %d (%q)", key, found, env)
	}
	sort.Strings(rest)
	return value, rest
}

func TestRunWardenTeardownHere(t *testing.T) {
	t.Run("a main instance spells its canonical target and hands the child no identity at all", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		runs := apiTestRecordOcwarden(t, 0, "torn down\n", false)

		exitCode, log, timedOut := api.runWardenTeardownHere("/tmp/ocwarden")

		if exitCode != 0 || log != "torn down\n" || timedOut {
			t.Fatalf("got (%d, %q, %v)", exitCode, log, timedOut)
		}
		if len(*runs) != 1 {
			t.Fatalf("want one child run, got %d (%v)", len(*runs), *runs)
		}
		run := (*runs)[0]
		if run.binPath != "/tmp/ocwarden" {
			t.Fatalf("binPath: %q", run.binPath)
		}
		apiTestWantEqual(t, "argv", run.args, []string{"teardown", "--canonical"})
		sorted := append([]string{}, run.env...)
		sort.Strings(sorted)
		apiTestWantEqual(t, "child env", sorted, apiTestAllowlistedChildEnv())
	})

	t.Run("a namespaced instance tears down its OWN warden and drops the namespace the parent env claimed", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		api.namespace = "bench"
		runs := apiTestRecordOcwarden(t, 0, "", false)

		api.runWardenTeardownHere("/tmp/ocwarden")

		run := (*runs)[0]
		apiTestWantEqual(t, "argv", run.args, []string{"teardown"})
		sorted := append([]string{}, run.env...)
		sort.Strings(sorted)
		apiTestWantEqual(t, "child env", sorted, []string{
			"HOME=/tmp/oc-home",
			"OC_CLAUDE_BIN=/opt/claude",
			"OC_CLAUDE_CRED_CHECK=0",
			"OC_CODEX_BIN=/opt/codex",
			"OC_NAMESPACE=bench",
			"PATH=/usr/bin:/bin",
		})
	})

	t.Run("whatever the child answers is passed back untouched", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		apiTestArmOcwardenEnv(t)
		apiTestRecordOcwarden(t, -1, "still running\n", true)

		exitCode, log, timedOut := api.runWardenTeardownHere("/tmp/ocwarden")

		if exitCode != -1 || log != "still running\n" || !timedOut {
			t.Fatalf("got (%d, %q, %v)", exitCode, log, timedOut)
		}
	})
}

func TestTeardownHereForeignTargetMsg(t *testing.T) {
	got := teardownHereForeignTargetMsg("m-studio")

	if got != "teardown-here only ever tears down the warden running on THIS server "+
		"host — it carries no machine selector, so it cannot reach m-studio"+
		"; refusing rather than destroying this host's daemon under another "+
		"machine's name. To retire a different machine, use POST "+
		"/api/machines/{member_id}/uninstall (the remote uninstall the target's "+
		"own warden executes) and then DELETE /api/machines/{member_id}. To "+
		"repair this host's own warden, use install_warden_on_server_host — it "+
		"runs `ocwarden install --force`, which overwrites an existing install, "+
		"so nothing has to be torn down first." {
		t.Fatalf("refusal: %q", got)
	}
}

func TestTeardownHereRefusal(t *testing.T) {
	t.Run("the server-local machine is owed the undeletable refusal, not the foreign-target one", func(t *testing.T) {
		got := teardownHereRefusal(ServerSelfHost)

		if got != "the server-local machine cannot be deleted" {
			t.Fatalf("refusal: %q", got)
		}
	})

	t.Run("any other machine is owed the foreign-target refusal naming it, and never an empty answer", func(t *testing.T) {
		for _, machineID := range []string{"m-studio", "mbp5", ""} {
			got := teardownHereRefusal(machineID)
			if got != teardownHereForeignTargetMsg(machineID) {
				t.Fatalf("refusal for %q: %q", machineID, got)
			}
			if got == "" {
				t.Fatalf("no machine may proceed today, but %q was waved through", machineID)
			}
		}
	})
}

func TestHandleTeardownHereApiMachinesMachineIdTeardownHerePost(t *testing.T) {
	t.Run("the server-local machine is refused as undeletable and stays on the roster", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/teardown-here", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "the server-local machine cannot be deleted")
		dashboard.wantFrames()
		apiTestWantRosterStatus(t, d, ServerSelfHost, "active")
		apiTestWantDesiredState(t, d, ServerSelfHost, "offline")
	})

	t.Run("a machine other than the server's own is refused and sent to the retire verbs instead", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/teardown-here", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"teardown-here only ever tears down the warden running on THIS server host — "+
				"it carries no machine selector, so it cannot reach "+machineID+
				"; refusing rather than destroying this host's daemon under another "+
				"machine's name. To retire a different machine, use POST "+
				"/api/machines/{member_id}/uninstall (the remote uninstall the target's "+
				"own warden executes) and then DELETE /api/machines/{member_id}. To "+
				"repair this host's own warden, use install_warden_on_server_host — it "+
				"runs `ocwarden install --force`, which overwrites an existing install, "+
				"so nothing has to be torn down first.")
		dashboard.wantFrames()
		apiTestWantRosterStatus(t, d, machineID, "active")
	})

	t.Run("a machine id nothing carries answers 404 naming it and tears nothing down", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/teardown-here", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/teardown-here", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiTestWantRosterStatus(t, d, ServerSelfHost, "active")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.ocwardenFS = fstest.MapFS{}

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/teardown-here", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUninstallMachineApiMachinesMemberIdUninstallPost(t *testing.T) {
	t.Run("uninstalling a machine that is connected arms the uninstall intent and fans it to the dashboard and to that machine", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		self := apiTestListen(t, api, machineID)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": true,
		})
		frame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "uninstall",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("uninstalling a machine nothing is connected from dispatches nothing and leaves the intent offline", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/uninstall", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": false,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id":            machineID,
					"name":          "Studio Mac",
					"status":        "active",
					"desired_state": "offline",
					"owner_id":      "owner",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a machine id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/uninstall", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/m-server-self/uninstall", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func apiTestOnboardMachine(t *testing.T, h http.Handler, owner, displayName string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/machines", owner, `{"display_name":"`+displayName+`"}`)
	if status != 200 {
		t.Fatalf("onboard machine: %d %v", status, data)
	}
	id, _ := data["machine_id"].(string)
	if id == "" {
		t.Fatalf("onboard machine must mint a machine id: %v", data)
	}
	return id
}

func TestHandleUpgradeMachineApiMachinesMemberIdUpgradePost(t *testing.T) {
	t.Run("a machine holding a live downstream is handed the update verb and nothing durable is written", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/upgrade", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": true,
		})
		apiTestWantWardenCommands(t, api, machineID,
			`data: {"topic":"warden-command","data":{"rpc":"update","args":{"member_id":"`+machineID+`"}}}`+"\n\n")
		dashboard.wantFrames()
		apiTestWantDesiredState(t, d, machineID, "offline")
	})

	t.Run("a machine with no live downstream is dispatched nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/upgrade", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"dispatched": false,
		})
		apiTestWantWardenCommands(t, api, machineID)
		dashboard.wantFrames()
		apiTestWantDesiredState(t, d, machineID, "offline")
	})

	t.Run("a machine id nothing carries answers 404 naming it and enqueues nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/nope/upgrade", owner, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'nope' not found")
		apiTestWantWardenCommands(t, api, "nope")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		apiTestListen(t, api, machineID)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/machines/"+machineID+"/upgrade", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWardenCommands(t, api, machineID)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/machines/"+ServerSelfHost+"/upgrade", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWardenCommands(t, api, ServerSelfHost)
	})
}

// apiTestWantWardenCommands drains machineID's warden command FIFO — the same
// queue the machine's SSE loop collects from — and asserts it held exactly the
// frames named, in order. Passing none asserts nothing was enqueued at all.
func apiTestWantWardenCommands(t *testing.T, api *apiServer, machineID string, want ...string) {
	t.Helper()
	pending := api.hub.DrainWardenCommands(machineID)
	if len(pending) != len(want) {
		t.Fatalf("warden commands: want %d, got %d (%q)", len(want), len(pending), pending)
	}
	for i, w := range want {
		if got := string(pending[i].Frame); got != w {
			t.Fatalf("warden command %d: want %q, got %q", i, w, got)
		}
		if pending[i].Subject != machineID {
			t.Fatalf("warden command %d subject: want %q, got %q", i, machineID, pending[i].Subject)
		}
	}
}

func TestHandleDeleteMachineApiMachinesMemberIdDelete(t *testing.T) {
	t.Run("deleting a machine answers the removal and fans a member removal carrying no payload", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		machineID := apiTestOnboardMachine(t, h, owner, "Studio Mac")
		dashboard := apiTestListen(t, api, "")
		self := apiTestListen(t, api, machineID)
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/machines/"+machineID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"member_id":  machineID,
			"machine_id": machineID,
			"removed":    true,
		})
		frame := map[string]any{
			"seq":   2,
			"topic": "member",
			"op":    "remove",
			"data": map[string]any{
				"entity":  "member",
				"key":     "owner::" + machineID,
				"epoch":   2,
				"deleted": true,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		self.wantFrames(frame)
		bystander.wantFrames()
	})

	t.Run("a member that is not a warden answers 409 naming the kind it is and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/machines/kip", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "member 'kip' is not a warden machine (kind='staff')")
		dashboard.wantFrames()
	})

	t.Run("the server-local machine answers 409 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/machines/m-server-self", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "the server-local machine cannot be deleted")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "DELETE", "/api/machines/m-server-self", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateAccountApiAccountsAccountIdPatch(t *testing.T) {
	t.Run("an overlay over a compound account tag answers the alias and is what the station stores", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/claude%2F9f2c", owner, `{"display_name":"  Studio plan  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             "claude/9f2c",
			"display_name":   "Studio plan",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		apiTestWantAccountAliases(t, d, map[string]string{"claude/9f2c": "Studio plan"})
		dashboard.wantFrames()
	})

	t.Run("an ordinary agent identity may set one because this row's floor is machine", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", agent, `{"display_name":"Bench"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             "codex",
			"display_name":   "Bench",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		apiTestWantAccountAliases(t, d, map[string]string{"codex": "Bench"})
	})

	t.Run("a body naming no display name answers 422 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name is required")
		apiTestWantAccountAliases(t, d, map[string]string{})
	})

	t.Run("a display name that is only whitespace answers 422 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", owner, `{"display_name":"   "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name cannot be blank")
		apiTestWantAccountAliases(t, d, map[string]string{})
	})

	t.Run("a body carrying a field the shape does not declare answers 422 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", owner, `{"display_name":"Bench","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "note"`)
		apiTestWantAccountAliases(t, d, map[string]string{})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/accounts/codex", "", `{"display_name":"Bench"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantAccountAliases(t, d, map[string]string{})
	})
}

// apiTestWantAccountAliases asserts the whole stored account-overlay table —
// the durable half of this row's answer, which reaches no other endpoint.
func apiTestWantAccountAliases(t *testing.T, d *DAL, want map[string]string) {
	t.Helper()
	got, err := d.AccountDisplayNames()
	if err != nil {
		t.Fatalf("AccountDisplayNames: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("account aliases: want %v, got %v", want, got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("account alias %q: want %q, got %q", k, v, got[k])
		}
	}
}

func TestHandleUpdateMachineApiMachinesMachineIdPatch(t *testing.T) {
	t.Run("an overlay answers the alias and is the name the machine roster then shows", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{"display_name":"  Loft mini  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             ServerSelfHost,
			"display_name":   "Loft mini",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "Loft mini")
		dashboard.wantFrames()
	})

	t.Run("an overlay may name a machine the roster does not carry", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/m-never-onboarded", owner, `{"display_name":"Spare"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":             "m-never-onboarded",
			"display_name":   "Spare",
			"owner_id":       "owner",
			"schema_version": 3,
		})
		names, err := d.MachineDisplayNames()
		if err != nil {
			t.Fatalf("MachineDisplayNames: %v", err)
		}
		if names["m-never-onboarded"] != "Spare" {
			t.Fatalf("stored overlays: %v", names)
		}
	})

	t.Run("a body naming no display name answers 422 and leaves the roster name alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name is required")
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})

	t.Run("a display name that is only whitespace answers 422 and leaves the roster name alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{"display_name":"   "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_name cannot be blank")
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})

	t.Run("a body carrying a field the shape does not declare answers 422 and leaves the roster name alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, owner, `{"display_name":"Loft","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "note"`)
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/machines/"+ServerSelfHost, "", `{"display_name":"Loft"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantMachineDisplayName(t, h, owner, ServerSelfHost, "伺服器這一台")
	})
}

// apiTestWantMachineDisplayName reads the overlay back the way the cockpit
// does — through the machine roster, where the alias folds over the member
// name — so a stored overlay is observed on the surface that consumes it.
func apiTestWantMachineDisplayName(t *testing.T, h http.Handler, owner, machineID, want string) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/machines", owner, "")
	if rec.Code != 200 {
		t.Fatalf("list machines: %d (%s)", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	for _, row := range rows {
		if row["machine_id"] == machineID {
			if row["display_name"] != want {
				t.Fatalf("display_name: want %q, got %q", want, row["display_name"])
			}
			return
		}
	}
	t.Fatalf("machine %q is not in the roster: %v", machineID, rows)
}

func TestHandleInstallScriptInstallShGet(t *testing.T) {
	t.Run("the claim-code installer is served verbatim as plain text to a caller carrying no identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/install.sh?code=one-time-claim-code-placeholder", "", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("content-type: %q", got)
		}
		if got := rec.Body.String(); got != apiTestClaimCodeInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})

	t.Run("the legacy query-parameter installer is served verbatim", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/install.sh?token=legacy-credential-placeholder", "", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("content-type: %q", got)
		}
		if got := rec.Body.String(); got != apiTestLegacyInstaller {
			t.Fatalf("script:\n%s", got)
		}
	})

	t.Run("naming neither parameter answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/install.sh", "", "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "exactly one of ?code= or ?token= is required")
	})

	t.Run("naming both parameters answers the same 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h,
			"GET", "/install.sh?code=one-time-claim-code-placeholder&token=legacy-credential-placeholder", "", "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "exactly one of ?code= or ?token= is required")
	})
}

const apiTestClaimCodeInstaller = `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL 'https://example.com/install.sh?code=<one-time code>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — claims the machine token just below, then pulls the ocwarden binary.
#   sed  — extracts the token from the claim response JSON.
for tool in tmux curl sed; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Probe the warden binary availability BEFORE redeeming the one-time claim
# code — a server that cannot serve the binary (503) must not burn the code.
if ! curl -fsI "https://example.com/api/warden/binary" >/dev/null 2>&1; then
  echo "Error: the server cannot serve the warden binary (https://example.com/api/warden/binary is unavailable)." >&2
  echo "Fix: redeploy the server with the prebuilt binaries (bin/ocwarden) or an embed-carrying build, then re-run this one-liner — the install code was NOT consumed." >&2
  exit 1
fi

# Exchange the ONE-TIME claim code for this machine's real exec-token FIRST —
# before any download — so an expired/used install link fails at the earliest
# possible point. The code is single-use: a replayed one-liner lands here.
if ! CLAIM_RESPONSE="$(curl -fsS -X POST "https://example.com/api/machines/claim" \
  -H 'Content-Type: application/json' --data '{"code":"one-time-claim-code-placeholder"}')"; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi
# The token is a base64url JWT — no quote/backslash can appear inside it.
OC_TOKEN="$(printf '%s' "$CLAIM_RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
if [ -z "$OC_TOKEN" ]; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the claimed token authorizes the install, not this fetch).
curl -fsSL "https://example.com/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_BASE="https://example.com" OC_TOKEN="$OC_TOKEN" ./ocwarden install --force
`

const apiTestLegacyInstaller = `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL 'https://example.com/install.sh?token=<jwt>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — used just below to pull the ocwarden binary.
for tool in tmux curl; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the boot token authorizes the install, not this fetch).
curl -fsSL "https://example.com/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_BASE="https://example.com" OC_TOKEN="legacy-credential-placeholder" ./ocwarden install --force
`

func TestServeBinary(t *testing.T) {
	t.Run("the embedded copy is streamed as an attachment", func(t *testing.T) {
		embedded := fstest.MapFS{"ocwarden": &fstest.MapFile{Data: []byte("warden bytes")}}
		rec := httptest.NewRecorder()

		serveBinary(rec, httptest.NewRequest("GET", "/api/warden/binary", nil), "ocwarden", embedded)

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got != "warden bytes" {
			t.Fatalf("body: %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("content-type: %q", got)
		}
		if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="ocwarden"` {
			t.Fatalf("content-disposition: %q", got)
		}
	})

	t.Run("a build carrying no embedded copy answers 503", func(t *testing.T) {
		rec := httptest.NewRecorder()

		serveBinary(rec, httptest.NewRequest("GET", "/api/warden/binary", nil), "ocwarden", fstest.MapFS{})

		if rec.Code != 503 {
			t.Fatalf("want 503, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, map[string]any{
			"error": map[string]any{
				"code":    "service_unavailable",
				"message": "ocwarden binary is not available (no embedded copy in this server build)",
			},
		})
		if got := rec.Header().Get("Content-Disposition"); got != "" {
			t.Fatalf("content-disposition: %q", got)
		}
	})
}

func TestHandleWardenBinaryApiWardenBinaryGet(t *testing.T) {
	t.Run("the prebuilt warden is streamed as an attachment to a caller carrying no identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		apiTestWantBinaryDownload(t, apiRequest(t, h, "GET", "/api/warden/binary", "", ""), "ocwarden")
	})
}

// apiTestWantBinaryDownload asserts the whole download envelope for a prebuilt
// artifact. The bytes themselves are whatever this build staged, so what is
// pinned is that they arrived intact and are declared as a download of exactly
// this file.
func apiTestWantBinaryDownload(t *testing.T, rec *httptest.ResponseRecorder, filename string) {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("content-type: %q", got)
	}
	if got, want := rec.Header().Get("Content-Disposition"), `attachment; filename="`+filename+`"`; got != want {
		t.Fatalf("content-disposition: want %q, got %q", want, got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("accept-ranges: %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("%s was served empty", filename)
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
		t.Fatalf("content-length: want %q, got %q", want, got)
	}
}

func TestHandleAgentBinaryApiAgentBinaryGet(t *testing.T) {
	t.Run("the prebuilt agent is streamed as an attachment to a caller carrying no identity", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		apiTestWantBinaryDownload(t, apiRequest(t, h, "GET", "/api/agent/binary", "", ""), "ocagent")
	})
}

// apiTestNamespacedLegacyInstaller is the legacy-token installer a NAMESPACED
// instance serves, reached over a base whose host makes the scheme plain http.
const apiTestNamespacedLegacyInstaller = `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL 'http://localhost:8848/install.sh?token=<jwt>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — used just below to pull the ocwarden binary.
for tool in tmux curl; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the boot token authorizes the install, not this fetch).
curl -fsSL "http://localhost:8848/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_NAMESPACE="bench" OC_BASE="http://localhost:8848" OC_TOKEN="legacy-credential-placeholder" ./ocwarden install --force
`

// apiTestNamespacedClaimCodeInstaller is apiTestNamespacedLegacyInstaller's
// claim-code twin: the same namespace prefix and base, over the script that
// redeems a one-time code.
const apiTestNamespacedClaimCodeInstaller = `#!/usr/bin/env bash
# officraft — one-line remote warden installer (served by GET /install.sh).
# Usage: curl -fsSL 'http://localhost:8848/install.sh?code=<one-time code>' | bash
set -euo pipefail

# Precheck: only the KEY tools the install truly needs (not an exhaustive audit).
#   tmux — the warden spawns each member's session through it (auto-installed
#          via Homebrew when available).
#   curl — claims the machine token just below, then pulls the ocwarden binary.
#   sed  — extracts the token from the claim response JSON.
for tool in tmux curl sed; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  # tmux is the one tool worth auto-installing: Homebrew boxes get it hands-free.
  if [ "$tool" = tmux ] && command -v brew >/dev/null 2>&1; then
    echo "tmux not found — installing via Homebrew..."
    brew install tmux || true
    if command -v tmux >/dev/null 2>&1; then
      continue
    fi
  fi
  echo "Error: $tool is required, please install it first" >&2
  echo "Fix: install it, then re-run this one-liner:" >&2
  echo "  macOS:  brew install $tool" >&2
  echo "  Linux:  sudo apt-get install -y $tool (or your distro's package manager)" >&2
  exit 1
done

# Probe the warden binary availability BEFORE redeeming the one-time claim
# code — a server that cannot serve the binary (503) must not burn the code.
if ! curl -fsI "http://localhost:8848/api/warden/binary" >/dev/null 2>&1; then
  echo "Error: the server cannot serve the warden binary (http://localhost:8848/api/warden/binary is unavailable)." >&2
  echo "Fix: redeploy the server with the prebuilt binaries (bin/ocwarden) or an embed-carrying build, then re-run this one-liner — the install code was NOT consumed." >&2
  exit 1
fi

# Exchange the ONE-TIME claim code for this machine's real exec-token FIRST —
# before any download — so an expired/used install link fails at the earliest
# possible point. The code is single-use: a replayed one-liner lands here.
if ! CLAIM_RESPONSE="$(curl -fsS -X POST "http://localhost:8848/api/machines/claim" \
  -H 'Content-Type: application/json' --data '{"code":"one-time-claim-code-placeholder"}')"; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi
# The token is a base64url JWT — no quote/backslash can appear inside it.
OC_TOKEN="$(printf '%s' "$CLAIM_RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
if [ -z "$OC_TOKEN" ]; then
  echo "Error: this install link has expired or was already used." >&2
  echo "Fix: open the cockpit -> Machines -> boot command, and run the fresh one-liner." >&2
  exit 1
fi

# Pull the prebuilt ocwarden binary from the PUBLIC binary endpoint (no auth
# header needed — the claimed token authorizes the install, not this fetch).
curl -fsSL "http://localhost:8848/api/warden/binary" -o ocwarden
chmod +x ocwarden

# Install the warden with the server-templated identity. --force makes a re-install
# ALWAYS OVERWRITE any prior warden on the box (後裝永遠覆蓋前裝).
OC_NAMESPACE="bench" OC_BASE="http://localhost:8848" OC_TOKEN="$OC_TOKEN" ./ocwarden install --force
`
