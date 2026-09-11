// Skeleton generated from server/ocserverd/routes.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestVerifyDiffShareSig(t *testing.T) {
	key := []byte("active")
	keys := newKeyring([]signingKey{{ID: "k-active", Key: key}}, "k-active")
	before := " padded-before "
	after := "doc:global_context/global/current/text"
	labelBefore := "初始欄"
	labelAfter := "目前欄"
	sig := diffSigFor(key, before, after, labelBefore, labelAfter)

	request := func(b, a, lb, la string) *http.Request {
		q := url.Values{
			diffParamBefore:     {b},
			diffParamAfter:      {a},
			diffParamLabelBefor: {lb},
			diffParamLabelAfter: {la},
		}
		return httptest.NewRequest(http.MethodGet, "/api/diff?"+q.Encode(), nil)
	}

	if !verifyDiffShareSig(keys, request(before, after, labelBefore, labelAfter), sig) {
		t.Fatal("a signature for all four raw comparison fields was refused")
	}

	for _, tc := range []struct {
		name        string
		before      string
		after       string
		labelBefore string
		labelAfter  string
	}{
		{name: "changed before address", before: "padded-before", after: after, labelBefore: labelBefore, labelAfter: labelAfter},
		{name: "changed after address", before: before, after: "doc:other", labelBefore: labelBefore, labelAfter: labelAfter},
		{name: "changed before label", before: before, after: after, labelBefore: "初始", labelAfter: labelAfter},
		{name: "changed after label", before: before, after: after, labelBefore: labelBefore, labelAfter: "現在欄"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if verifyDiffShareSig(keys, request(tc.before, tc.after, tc.labelBefore, tc.labelAfter), sig) {
				t.Fatalf("changed comparison field was accepted: %#v", tc)
			}
		})
	}
}

func TestRouteSpecs(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	specs := routeSpecs(&ServerInterfaceWrapper{Handler: api})
	if len(specs) != 186 {
		t.Fatalf("routeSpecs returned %d rows, want 186", len(specs))
	}

	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		key := spec.Method + " " + spec.Path
		if seen[key] {
			t.Fatalf("route table contains duplicate row %q", key)
		}
		seen[key] = true
		if spec.Handler == nil {
			t.Fatalf("route %q has no generated wrapper handler", key)
		}
		if spec.Summary == "" {
			t.Fatalf("route %q has no summary", key)
		}
		switch spec.Auth {
		case authPublic:
			if spec.Requires != requiresPublic {
				t.Fatalf("public route %q requires %s, want public", key, spec.Requires)
			}
		case authGated:
			if _, ok := principalRank[spec.Requires]; !ok {
				t.Fatalf("gated route %q has unknown principal floor %s", key, spec.Requires)
			}
		default:
			t.Fatalf("route %q has unknown auth class %q", key, spec.Auth)
		}
	}

	for _, tc := range []struct {
		method   string
		path     string
		auth     string
		requires principalClass
		exclude  bool
		mcpTool  string
	}{
		{method: http.MethodGet, path: "/api/version", auth: authPublic, requires: requiresPublic},
		{method: http.MethodPost, path: "/api/mcp", auth: authGated, requires: principalMachine, exclude: true},
		{method: http.MethodGet, path: "/api/release/check", auth: authGated, requires: principalAdminAgent, mcpTool: "check_release"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var got *RouteSpec
			for i := range specs {
				if specs[i].Method == tc.method && specs[i].Path == tc.path {
					got = &specs[i]
					break
				}
			}
			if got == nil {
				t.Fatalf("route %s %s is missing", tc.method, tc.path)
			}
			if got.Auth != tc.auth || got.Requires != tc.requires || got.MCPExclude != tc.exclude || got.MCPTool != tc.mcpTool {
				t.Fatalf("route row = %#v, want auth=%q requires=%s exclude=%v mcp_tool=%q", got, tc.auth, tc.requires, tc.exclude, tc.mcpTool)
			}
		})
	}
}
