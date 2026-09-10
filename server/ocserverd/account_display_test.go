// Skeleton generated from server/ocserverd/account_display.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAccountLabelOverlay(t *testing.T) {
	telemetry := map[string]map[string]any{
		"member": {
			"account":       "acct-shared",
			"account_label": "older@example.com",
			"ts":            100.0,
		},
		"outsource": {
			"account":       "acct-shared",
			"account_label": "newer@example.com",
			"ts":            200.0,
		},
		"second": {
			"account":       "acct-second",
			"account_label": "second@example.com",
			"ts":            150.0,
		},
		"unlabelled": {"account": "acct-empty", "ts": 300.0},
	}
	want := map[string]string{
		"acct-shared": "newer@example.com",
		"acct-second": "second@example.com",
	}
	if got := accountLabelOverlay(telemetry, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("accountLabelOverlay(owner) = %#v, want %#v", got, want)
	}
	if got := accountLabelOverlay(telemetry, false); got == nil || len(got) != 0 {
		t.Fatalf("accountLabelOverlay(non-owner) = %#v, want an empty map", got)
	}
}

func TestTelemetryAccount(t *testing.T) {
	tests := []struct {
		name    string
		entry   map[string]any
		runtime string
		want    string
	}{
		{name: "matching runtime", entry: map[string]any{"account": "acct-1", accountRuntimeKey: "claude"}, runtime: "claude", want: "acct-1"},
		{name: "runtime comparison is trimmed", entry: map[string]any{"account": "acct-1", accountRuntimeKey: " codex "}, runtime: " codex ", want: "acct-1"},
		{name: "different runtime is unreadable", entry: map[string]any{"account": "acct-1", accountRuntimeKey: "claude"}, runtime: "codex"},
		{name: "missing provenance is unreadable", entry: map[string]any{"account": "acct-1"}, runtime: "claude"},
		{name: "missing account is unreadable", entry: map[string]any{accountRuntimeKey: "claude"}, runtime: "claude"},
		{name: "wrong provenance type is unreadable", entry: map[string]any{"account": "acct-1", accountRuntimeKey: 1}, runtime: "claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := telemetryAccount(tt.entry, tt.runtime); got != tt.want {
				t.Fatalf("telemetryAccount(%#v, %q) = %q, want %q", tt.entry, tt.runtime, got, tt.want)
			}
		})
	}
}

func TestClearAccountPairing(t *testing.T) {
	entry := map[string]any{
		"account":         "acct-1",
		accountRuntimeKey: "claude",
		"account_label":   "acct@example.com",
		"other":           "keep me",
	}
	clearAccountPairing(entry)
	for _, key := range []string{"account", accountRuntimeKey, "account_label"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("clearAccountPairing left %q in %#v", key, entry)
		}
	}
	if entry["other"] != "keep me" {
		t.Fatalf("clearAccountPairing changed unrelated data: %#v", entry)
	}
}

func TestApplyAccountReport(t *testing.T) {
	ptr := func(v string) *string { return &v }
	tests := []struct {
		name  string
		entry map[string]any
		acct  any
		label any
		run   *string
		want  map[string]any
	}{
		{
			name:  "proven account stores normalized provenance and label",
			entry: map[string]any{"other": "keep"}, acct: "acct-1", label: "acct@example.com", run: ptr(" codex "),
			want: map[string]any{"other": "keep", "account": "acct-1", accountRuntimeKey: "codex", "account_label": "acct@example.com"},
		},
		{
			name:  "label-only report attaches to the standing pairing",
			entry: map[string]any{"account": "acct-1", accountRuntimeKey: "claude"}, label: "new@example.com", run: ptr("claude"),
			want: map[string]any{"account": "acct-1", accountRuntimeKey: "claude", "account_label": "new@example.com"},
		},
		{
			name:  "runtime switch retires the old pairing before writing the new one",
			entry: map[string]any{"account": "old", accountRuntimeKey: "claude", "account_label": "old@example.com"}, acct: "new", run: ptr("codex"),
			want: map[string]any{"account": "new", accountRuntimeKey: "codex"},
		},
		{
			name:  "account without runtime clears the whole pairing",
			entry: map[string]any{"account": "old", accountRuntimeKey: "claude", "account_label": "old@example.com"}, acct: "new", label: "new@example.com",
			want: map[string]any{},
		},
		{
			name:  "runtime-only heartbeat preserves the pairing",
			entry: map[string]any{"account": "acct-1", accountRuntimeKey: "claude", "account_label": "acct@example.com"}, run: ptr("claude"),
			want: map[string]any{"account": "acct-1", accountRuntimeKey: "claude", "account_label": "acct@example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyAccountReport(tt.entry, tt.acct, tt.label, tt.run)
			if !reflect.DeepEqual(tt.entry, tt.want) {
				t.Fatalf("entry = %#v, want %#v", tt.entry, tt.want)
			}
		})
	}
}

func TestResolveAccountDisplay(t *testing.T) {
	aliases := map[string]string{"acct-alias": "Owner alias", "acct-blank": ""}
	labels := map[string]string{"acct-alias": "Reporter label", "acct-blank": "Reporter label", "acct-label": "Reporter label"}
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "owner alias wins over reporter label", raw: "acct-alias", want: "Owner alias"},
		{name: "reporter label fills absent alias", raw: "acct-label", want: "Reporter label"},
		{name: "blank alias does not hide label", raw: "acct-blank", want: "Reporter label"},
		{name: "unknown account is blank", raw: "acct-unknown", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveAccountDisplay(aliases, labels, tt.raw); got != tt.want {
				t.Fatalf("resolveAccountDisplay(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestAccountDisplayFold(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	if err := d.PutAccountAlias(AccountAlias{Account: "acct-alias", DisplayName: "Owner alias"}); err != nil {
		t.Fatalf("PutAccountAlias: %v", err)
	}
	telemetry := map[string]map[string]any{
		"worker": {"account": "acct-label", "account_label": "worker@example.com", "ts": 100.0},
	}
	ownerRequest := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), claimsContextKey, map[string]any{"scope": "owner"}),
	)
	ownerFold, err := api.accountDisplayFold(ownerRequest, telemetry)
	if err != nil {
		t.Fatalf("owner accountDisplayFold: %v", err)
	}
	if got := ownerFold("acct-alias"); got != "Owner alias" {
		t.Fatalf("owner alias = %q, want Owner alias", got)
	}
	if got := ownerFold("acct-label"); got != "worker@example.com" {
		t.Fatalf("owner reporter label = %q, want worker@example.com", got)
	}

	agentRequest := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), claimsContextKey, map[string]any{"scope": "agent", "sub": apiTestPlainAgentID}),
	)
	agentFold, err := api.accountDisplayFold(agentRequest, telemetry)
	if err != nil {
		t.Fatalf("agent accountDisplayFold: %v", err)
	}
	if got := agentFold("acct-alias"); got != "Owner alias" {
		t.Fatalf("agent alias = %q, want Owner alias", got)
	}
	if got := agentFold("acct-label"); got != "" {
		t.Fatalf("agent reporter label = %q, want empty privacy-gated display", got)
	}
}
