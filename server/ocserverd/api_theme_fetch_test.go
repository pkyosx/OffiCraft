// Skeleton generated from server/ocserverd/api_theme_fetch.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestValidThemeFetchURL(t *testing.T) {
	t.Skip("TODO: validThemeFetchURL is the FORMAT check the owner did allow: the address must parse, must be absolute, and must be http/https.")
}

// themeFetchStub stands in for whatever is on the far end of a pasted link. The
// handler builds its client as &http.Client{Timeout: …} with no Transport, so
// http.DefaultTransport is the seam the outbound call actually travels through
// and swapping it keeps this test off every network there is.
type themeFetchStub struct {
	answer func(*http.Request) (*http.Response, error)
}

func (s themeFetchStub) RoundTrip(r *http.Request) (*http.Response, error) { return s.answer(r) }

// apiTestFarSide points the outbound theme fetch at a canned answer for the rest
// of this test, and hands back the requests the handler actually made.
func apiTestFarSide(t *testing.T, code int, body string) *[]*http.Request {
	t.Helper()
	seen := &[]*http.Request{}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = themeFetchStub{answer: func(r *http.Request) (*http.Response, error) {
		*seen = append(*seen, r)
		return &http.Response{
			StatusCode: code,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	}}
	return seen
}

// apiTestNoFarSide fails the test if the handler dials at all.
func apiTestNoFarSide(t *testing.T) {
	t.Helper()
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = themeFetchStub{answer: func(r *http.Request) (*http.Response, error) {
		t.Errorf("the handler dialled %s, and this request should never have left the process", r.URL)
		return nil, io.EOF
	}}
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

	t.Run("a link that answers a theme hands back the far side's bytes verbatim, and asks for JSON when it fetches them", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		const bundle = `{"id":"linked", "name":"Linked",  "colors":{"--color-bg":"#123456"}}`
		seen := apiTestFarSide(t, 200, bundle)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"https://example.test/theme.json"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"content": bundle})
		dashboard.wantFrames()

		if len(*seen) != 1 {
			t.Fatalf("want exactly one outbound fetch, got %d", len(*seen))
		}
		out := (*seen)[0]
		if out.Method != "GET" || out.URL.String() != "https://example.test/theme.json" {
			t.Fatalf("outbound request was %s %s", out.Method, out.URL)
		}
		if got := out.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("outbound Accept was %q", got)
		}
	})

	t.Run("fetching a theme does not save it — the link import is a read, and the owner still has to file it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestFarSide(t, 200, `{"id":"linked","name":"Linked","colors":{"--color-bg":"#123456"}}`)

		if status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"https://example.test/theme.json"}`); status != 200 {
			t.Fatalf("fetch: %d %v", status, data)
		}
		apiThemeAbsent(t, h, owner, "linked")
		apiThemeList(t, h, owner)
	})

	t.Run("a link that answers a non-200 refuses with 502 quoting the status the far side gave", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestFarSide(t, 404, "no such file")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"https://example.test/theme.json"}`)
		if status != 502 {
			t.Fatalf("want 502, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", "that link answered 404")
		dashboard.wantFrames()
	})

	t.Run("a link whose content is not JSON at all is refused 422, not blamed on the far side", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestFarSide(t, 200, "not json")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"https://example.test/theme.json"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"that link's content is not a theme bundle: invalid character 'o' in literal null (expecting 'u')")
		dashboard.wantFrames()
	})

	t.Run("a link whose content is JSON but not an admissible theme is refused 422 naming the rule it broke", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestFarSide(t, 200, `{"id":"linked","name":"Linked","colors":{}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"https://example.test/theme.json"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"that link's content is not a valid theme: custom_themes[0]: colors must hold 1..200 entries (got 0)")
		dashboard.wantFrames()
	})

	t.Run("a body one byte past the four-megabyte ceiling is refused 422 rather than handed back truncated", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestFarSide(t, 200, strings.Repeat("x", int(themeFetchMaxBytes)+1))
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner,
			`{"url":"https://example.test/theme.json"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"that link's content is larger than the 4194304-byte limit for a theme")
		dashboard.wantFrames()
	})

	t.Run("an address that is not an absolute http or https link is refused 422 before anything is dialled", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestNoFarSide(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner, `{"url":"ftp://example.test/theme.json"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url must be an absolute http:// or https:// link")
		dashboard.wantFrames()
	})

	t.Run("a body with no url at all is refused 422 naming the missing field, before anything is dialled", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestNoFarSide(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: url")
		dashboard.wantFrames()
	})

	t.Run("a POST /api/theme/fetch request the wire layer rejects (a malformed JSON body) answers 422 without reaching the domain", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestNoFarSide(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401 before anything is dialled", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		apiTestNoFarSide(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", "", `{"url":"https://example.test/theme.json"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent, before anything is dialled", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestNoFarSide(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/theme/fetch", housekeeper, `{"url":"https://example.test/theme.json"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})
}
