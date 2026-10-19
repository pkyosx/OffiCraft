package main

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecretRevealUplinkBody drives `secret run` against a live listener and
// compares the reveal body it sent against the frozen SecretRevealRequestDTO.
func TestSecretRevealUplinkBody(t *testing.T) {
	revealDeclared := frozenIngestProperties(t, "SecretRevealRequestDTO")
	var posts []capturedPost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		posts = append(posts, capturedPost{path: r.URL.Path, auth: r.Header.Get("Authorization"), body: string(raw)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"secrets":[{"id":"sec-000000000001","name":"n","note":"","value":"fake-value-1"}]}`))
	}))
	t.Cleanup(srv.Close)
	cfg := Config{BaseConfigured: true, Base: srv.URL, Token: "t"}

	var out, errOut bytes.Buffer
	rc := cmdSecretRun(srv.Client(), cfg, []string{"A=sec-000000000001"}, []string{"true"}, nil, nil, &out, &errOut)
	if rc != 0 {
		t.Fatalf("cmdSecretRun returned %d (stderr %q), want 0", rc, errOut.String())
	}
	if len(posts) != 1 {
		t.Fatalf("sent %v, want exactly one reveal", posts)
	}
	reveal := posts[0]
	if bad := schemaViolations(reveal.body, revealDeclared); len(bad) > 0 {
		t.Errorf("reveal body has keys the frozen schema refuses %v; body=%s", bad, reveal.body)
	}
	if want := `{"ids":["sec-000000000001"]}`; reveal.body != want {
		t.Errorf("reveal body = %s, want %s", reveal.body, want)
	}

	walked := map[string]int{reveal.path: 1}
	committed := manifestUplinkPaths(t, "cli/ocagent/secret_wire_test.go")
	if !maps.Equal(walked, committed) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but secret run posted %v", committed, walked)
	}
}
