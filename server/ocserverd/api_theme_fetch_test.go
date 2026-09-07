// Skeleton generated from server/ocserverd/api_theme_fetch.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestValidThemeFetchURL(t *testing.T) {
	t.Skip("TODO: validThemeFetchURL is the FORMAT check the owner did allow: the address must parse, must be absolute, and must be http/https.")
}

func TestHandleFetchThemeApiThemeFetchPost(t *testing.T) {
	t.Run("a link nothing answers on refuses with 502", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"http://127.0.0.1:1/theme.json"}`)

		if status != 502 {
			t.Fatalf("want 502, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error",
			`could not fetch that link: Get "http://127.0.0.1:1/theme.json": `+
				`dial tcp 127.0.0.1:1: connect: connection refused`)
		dashboard.wantFrames()
	})

	t.Run("a well-formed POST /api/theme/fetch answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/theme/fetch request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/theme/fetch reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/theme/fetch request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
