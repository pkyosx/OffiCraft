package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func fetch(t *testing.T, h http.Handler, method, path string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body), rec.Header().Get("Content-Type")
}

func spaFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>SPA SHELL</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
}

func TestFallbackServesStagedSPA(t *testing.T) {
	h := newFallbackHandler(defaultRouteSpecs(), spaFS())

	// "/" and any client-side route answer the shell (the SPA catch-all).
	for _, path := range []string{"/", "/settings", "/members/kyle"} {
		status, body, _ := fetch(t, h, "GET", path)
		if status != 200 || !strings.Contains(body, "SPA SHELL") {
			t.Fatalf("%s: want the SPA shell, got %d %q", path, status, body)
		}
	}

	// A real static asset serves as itself, never the shell.
	status, body, _ := fetch(t, h, "GET", "/assets/app.js")
	if status != 200 || !strings.Contains(body, "console.log") {
		t.Fatalf("asset: got %d %q", status, body)
	}

	// An asset-like miss stays an honest 404 (never rewritten to the shell).
	status, body, _ = fetch(t, h, "GET", "/assets/gone.png")
	if status != 404 || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("asset miss: want 404 envelope, got %d %q", status, body)
	}

	// An unknown API path is an API error, never HTML.
	status, body, _ = fetch(t, h, "GET", "/api/typo")
	if status != 404 || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("/api/typo: want 404 envelope, got %d %q", status, body)
	}

	// A wrong-method hit on a declared route template is a 405 envelope
	// (a right-method request would have matched the row's mux pattern).
	for _, probe := range [][2]string{
		{"DELETE", "/health"},
		{"PUT", "/api/members/kyle"},
		{"GET", "/api/self/waking"},
	} {
		status, body, _ = fetch(t, h, probe[0], probe[1])
		if status != 405 || !strings.Contains(body, `"code":"method_not_allowed"`) {
			t.Fatalf("%s %s: want 405 envelope, got %d %q", probe[0], probe[1], status, body)
		}
	}
}

func TestFallbackWithoutStagedBuildServesHint(t *testing.T) {
	h := newFallbackHandler(defaultRouteSpecs(), fstest.MapFS{})

	// "/" answers the friendly build hint (static.py _MISSING_DIST_HTML twin).
	status, body, ctype := fetch(t, h, "GET", "/")
	if status != 200 || !strings.Contains(body, "npm run build") ||
		!strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("hint: got %d %q %q", status, ctype, body)
	}

	// Everything else stays an honest 404 — no shell exists to serve.
	status, body, _ = fetch(t, h, "GET", "/settings")
	if status != 404 || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("/settings without build: want 404 envelope, got %d %q", status, body)
	}
}

func TestPathMatchesTemplate(t *testing.T) {
	cases := []struct {
		template, path string
		want           bool
	}{
		{"/api/members", "/api/members", true},
		{"/api/members/{member_id}", "/api/members/kyle", true},
		{"/api/members/{member_id}", "/api/members/", false},
		{"/api/members/{member_id}", "/api/members/kyle/activate", false},
		{"/api/lessons/{role_key}/{task_type}", "/api/lessons/writer/build", true},
		{"/api/machines/{machine_id}/boot-command", "/api/machines/mac1/boot-command", true},
		{"/api/machines/{machine_id}/boot-command", "/api/machines/mac1/uninstall", false},
	}
	for _, c := range cases {
		if got := pathMatchesTemplate(c.template, c.path); got != c.want {
			t.Fatalf("pathMatchesTemplate(%q, %q) = %v, want %v", c.template, c.path, got, c.want)
		}
	}
}
