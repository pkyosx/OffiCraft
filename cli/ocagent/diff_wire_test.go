package main

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiffShareLinkUplinkBody(t *testing.T) {
	mintDeclared := frozenIngestProperties(t, "DiffShareLinkCreateDTO")
	var posts []capturedPost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		posts = append(posts, capturedPost{path: r.URL.Path, auth: r.Header.Get("Authorization"), body: string(raw)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"/diff?after=att-ba9876543210&before=att-0123456789ab&link=tok","share_link":{}}`))
	}))
	t.Cleanup(srv.Close)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t"}

	var out, errOut bytes.Buffer
	rc := cmdDiff(srv.Client(), cfg, "att-0123456789ab", "att-ba9876543210", "Old", "New", true, &out, &errOut)
	if rc != 0 {
		t.Fatalf("cmdDiff returned %d (stderr %q), want 0", rc, errOut.String())
	}
	if len(posts) != 1 {
		t.Fatalf("sent %v, want exactly one mint", posts)
	}
	mint := posts[0]
	if bad := schemaViolations(mint.body, mintDeclared); len(bad) > 0 {
		t.Errorf("mint body has keys the frozen schema refuses %v; body=%s", bad, mint.body)
	}
	if want := `{"after":"att-ba9876543210","before":"att-0123456789ab","label_after":"New","label_before":"Old"}`; mint.body != want {
		t.Errorf("mint body = %s, want %s", mint.body, want)
	}

	walked := map[string]int{mint.path: 1}
	committed := manifestUplinkPaths(t, "cli/ocagent/diff_wire_test.go")
	if !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but diff --external posted %v", committed, walked)
	}
}
