// Skeleton generated from server/ocserverd/spa.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestWebdistFS(t *testing.T) {
	dist := webdistFS()
	data, err := fs.ReadFile(dist, ".gitkeep")
	if err != nil {
		t.Fatalf("webdistFS().ReadFile(.gitkeep): %v", err)
	}
	if string(data) != "" {
		t.Fatalf("webdistFS().ReadFile(.gitkeep) = %q, want an empty placeholder file", data)
	}
}

func TestPathMatchesTemplate(t *testing.T) {
	cases := []struct {
		name     string
		template string
		path     string
		want     bool
	}{
		{name: "literal path matches itself", template: "/api/health", path: "/api/health", want: true},
		{name: "one parameter matches one non-empty segment", template: "/api/members/{member_id}", path: "/api/members/kip", want: true},
		{name: "an empty parameter segment does not match", template: "/api/members/{member_id}", path: "/api/members/", want: false},
		{name: "a parameter does not absorb an extra segment", template: "/api/members/{member_id}", path: "/api/members/kip/webhooks", want: false},
		{name: "a literal segment must be equal", template: "/api/members/{member_id}", path: "/api/agents/kip", want: false},
		{name: "a trailing slash changes the segment count", template: "/api/health", path: "/api/health/", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathMatchesTemplate(tc.template, tc.path); got != tc.want {
				t.Fatalf("pathMatchesTemplate(%q, %q) = %v, want %v", tc.template, tc.path, got, tc.want)
			}
		})
	}
}

func TestNewFallbackHandler(t *testing.T) {
	specs := []RouteSpec{{Method: http.MethodGet, Path: "/api/members/{member_id}"}}
	dist := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("INDEX")},
		"assets/app.js": &fstest.MapFile{Data: []byte("APP")},
	}
	h := newFallbackHandler(specs, dist)

	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("a route-template hit with the wrong method answers the unified 405 envelope", func(t *testing.T) {
		rec := request(http.MethodPost, "/api/members/kip")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("want 405, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != `{"error":{"code":"method_not_allowed","message":"method not allowed"}}` {
			t.Fatalf("body = %q, want the complete method-not-allowed envelope", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q, want application/json", got)
		}
	})

	t.Run("an unknown API path stays an API 404 instead of receiving the SPA shell", func(t *testing.T) {
		rec := request(http.MethodGet, "/api/not-a-route")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != `{"error":{"code":"not_found","message":"not found"}}` {
			t.Fatalf("body = %q, want the complete not-found envelope", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q, want application/json", got)
		}
	})

	t.Run("an existing static asset is served as its own bytes", func(t *testing.T) {
		rec := request(http.MethodGet, "/assets/app.js")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != "APP" {
			t.Fatalf("body = %q, want %q", got, "APP")
		}
		if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
			t.Fatalf("content type = %q, want text/javascript; charset=utf-8", got)
		}
	})

	t.Run("a missing asset-like path answers 404 instead of being rewritten to index", func(t *testing.T) {
		rec := request(http.MethodGet, "/assets/missing.js")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != `{"error":{"code":"not_found","message":"not found"}}` {
			t.Fatalf("body = %q, want the complete not-found envelope", got)
		}
	})

	t.Run("a client-side route receives the SPA index", func(t *testing.T) {
		rec := request(http.MethodGet, "/settings/profile")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != "INDEX" {
			t.Fatalf("body = %q, want %q", got, "INDEX")
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("content type = %q, want text/html; charset=utf-8", got)
		}
	})

	t.Run("without an index the root answers the complete build hint", func(t *testing.T) {
		hint := newFallbackHandler(nil, fstest.MapFS{})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		hint.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != missingDistHTML {
			t.Fatalf("body = %q, want missingDistHTML", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("content type = %q, want text/html; charset=utf-8", got)
		}
	})

	t.Run("without an index a non-root client-side route stays a 404", func(t *testing.T) {
		hint := newFallbackHandler(nil, fstest.MapFS{})
		req := httptest.NewRequest(http.MethodGet, "/settings/profile", nil)
		rec := httptest.NewRecorder()
		hint.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != `{"error":{"code":"not_found","message":"not found"}}` {
			t.Fatalf("body = %q, want the complete not-found envelope", got)
		}
	})
}
