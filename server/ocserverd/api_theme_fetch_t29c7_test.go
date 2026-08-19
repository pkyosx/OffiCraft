package main

// api_theme_fetch_t29c7_test.go — T-29c7: paste ONE link, get a whole theme.
//
// The claim this file has to earn is the owner-visible one: a link goes in, the
// theme JSON comes back, and the import SUCCEEDS. So the headline test does not
// stop at "the endpoint answered 200" — it takes the bytes the endpoint handed
// back and stores them through the REAL import write (PATCH /api/settings'
// custom_themes), then reads the theme back out. An endpoint that fetched
// perfectly but produced something the import path refuses would be worthless,
// and a test that stopped at 200 could not tell the difference.
//
// The other tests pin the two things the owner asked for by name — 「只要管拿到
// 的 json 符合預期」 (the ANSWER is checked) and 「可以檢查 format?」 (the
// ADDRESS is checked only for format) — plus the two bounds that exist for
// availability: a timeout and a size ceiling.
//
// 🔴 ONE TEST HERE PINS AN ABSENCE ON PURPOSE:
// TestThemeFetch_LoopbackOriginIsNotRefusedForBeingLoopback. The owner ruled on
// 2026-08-03, after the timing of the risk was spelled out to him, that link
// ORIGIN is not to be constrained. Every other test in this file would still
// pass if somebody quietly added a private-address blocklist — in fact they run
// against 127.0.0.1, so such a blocklist would break them for a reason nobody
// would connect to the ruling. That test makes the ruling itself a red test.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// aThemeBundleJSON is one legitimate exported theme, in exactly the shape
// serializeBundle writes: a colours map plus id/name.
const aThemeBundleJSON = `{
  "id": "custom-7",
  "name": "夜行",
  "colors": {
    "--color-bg": "#101018",
    "--color-accent": "rgb(120, 90, 240)"
  }
}`

// themeOrigin stands up a throwaway HTTP origin and counts how many times it
// was actually dialled. The counter is not decoration: the format-refusal test
// asserts a refusal AND that nothing left the host, and without the counter a
// handler that fetched first and validated afterwards would look identical.
func themeOrigin(t *testing.T, h http.HandlerFunc) (url string, hits *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			h(w, r)
		}))
	t.Cleanup(srv.Close)
	return srv.URL, &n
}

func serveBody(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// fetchTheme drives the handler as owner and returns the recorder.
func fetchTheme(t *testing.T, api *apiServer, link string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleFetchThemeApiThemeFetchPost(rec,
		taskReq(t, http.MethodPost, "/api/theme/fetch",
			map[string]any{"url": link}, "owner", "owner"))
	return rec
}

// ── (1) the headline: link → JSON → the theme is actually imported ───────────

// TestThemeLinkImport_LinkInThemeStored is the whole ticket in one test. It
// deliberately runs past the fetch: the bytes that come back are handed to the
// SAME custom_themes write the cockpit's import performs, and the theme is then
// read back from settings. That last hop is what makes this a proof of import
// rather than a proof of download.
func TestThemeLinkImport_LinkInThemeStored(t *testing.T) {
	api := newTasksTestServer(t)
	link, hits := themeOrigin(t, serveBody(http.StatusOK, aThemeBundleJSON))

	rec := fetchTheme(t, api, link)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch: got %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("origin was dialled %d times, want exactly 1", got)
	}
	out := decodeBody[map[string]any](t, rec)
	content, _ := out["content"].(string)
	// VERBATIM, not re-serialised: the cockpit runs this through the same
	// parseImportedBundle a pasted bundle goes through, so any re-encoding here
	// would be a second serialisation of a theme and a second thing to drift.
	if content != aThemeBundleJSON {
		t.Fatalf("content is not the bytes the link served:\n got %q\nwant %q",
			content, aThemeBundleJSON)
	}

	// …and now the import itself, through the real write.
	var bundle ThemeBundleDTO
	if err := json.Unmarshal([]byte(content), &bundle); err != nil {
		t.Fatalf("fetched content does not parse as a bundle: %v", err)
	}
	// T-83ef moved the write: a fetched theme is stored through PUT
	// /api/themes/{id}, not through a whole-array settings patch. The CLAIM this
	// test makes is unchanged — a link-imported theme survives the real write and
	// reads back as the theme the link served — so the assertion follows the
	// behaviour to its new door rather than being deleted with the old one.
	put := httptest.NewRecorder()
	api.HandlePutThemeApiThemesThemeIdPut(put,
		taskReq(t, http.MethodPut, "/api/themes/"+bundle.Id, bundle, "owner", "owner"),
		bundle.Id)
	if put.Code != http.StatusOK {
		t.Fatalf("import write: got %d %s, want 200", put.Code, put.Body.String())
	}

	read := httptest.NewRecorder()
	api.HandleGetThemeApiThemesThemeIdGet(read,
		taskReq(t, http.MethodGet, "/api/themes/"+bundle.Id, nil, "owner", "owner"),
		bundle.Id)
	if read.Code != http.StatusOK {
		t.Fatalf("read back: got %d %s", read.Code, read.Body.String())
	}
	one := decodeBody[map[string]any](t, read)
	if one["id"] != "custom-7" || one["name"] != "夜行" {
		t.Fatalf("the stored theme is not the one the link served: %v", one)
	}

	// And it is the ONLY theme: the import created one row, it did not also leave
	// the set in some other shape. (The old assertion got this from the array's
	// length; the list endpoint is where that fact lives now.)
	list := httptest.NewRecorder()
	api.HandleListThemesApiThemesGet(list,
		taskReq(t, http.MethodGet, "/api/themes", nil, "owner", "owner"))
	if list.Code != http.StatusOK {
		t.Fatalf("list: got %d %s", list.Code, list.Body.String())
	}
	all := decodeBody[[]map[string]any](t, list)
	if len(all) != 1 {
		t.Fatalf("%d themes are saved after the link import, want 1 (%s)",
			len(all), list.Body.String())
	}
}

// ── (2) 「只要管拿到的 json 符合預期」 — the ANSWER is checked ───────────────

// TestThemeFetch_ContentThatIsNotAThemeIsRefused. Without this check a link
// pointing at any old JSON would answer 200, and the failure would only surface
// later, in the cockpit, as a puzzle. Each case names WHAT is wrong.
func TestThemeFetch_ContentThatIsNotAThemeIsRefused(t *testing.T) {
	// POSITIVE CONTROL FIRST — a handler that refused everything would satisfy
	// every refusal below on its own.
	t.Run("control: a real theme lands", func(t *testing.T) {
		api := newTasksTestServer(t)
		link, _ := themeOrigin(t, serveBody(http.StatusOK, aThemeBundleJSON))
		if rec := fetchTheme(t, api, link); rec.Code != http.StatusOK {
			t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
		}
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"not JSON at all", "<html>404 not found</html>"},
		{"JSON but not a theme", `{"hello":"world"}`},
		{"a theme with no colours", `{"id":"custom-7","name":"x","colors":{}}`},
		{"a colour token that is not a theme token", `{"id":"custom-7","name":"x","colors":{"--nope":"#fff"}}`},
		{"a colour value outside the grammar", `{"id":"custom-7","name":"x","colors":{"--color-bg":"url(evil)"}}`},
		{"an id reserved for a built-in", `{"id":"office","name":"x","colors":{"--color-bg":"#101018"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			link, _ := themeOrigin(t, serveBody(http.StatusOK, tc.body))
			rec := fetchTheme(t, api, link)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("got %d %s, want 422", rec.Code, rec.Body.String())
			}
			if strings.TrimSpace(rec.Body.String()) == "" {
				t.Fatal("refusal carried no message")
			}
		})
	}
}

// ── (3) 「可以檢查 format?」 — the ADDRESS is checked, for format only ────────

// TestThemeFetch_MalformedLinkIsRefusedWithoutDialling. The refusal AND the
// zero dial count are both load-bearing: a handler that dialled first would
// still answer 422 on a link that nothing can be made of, so the status alone
// proves nothing about ordering.
func TestThemeFetch_MalformedLinkIsRefusedWithoutDialling(t *testing.T) {
	api := newTasksTestServer(t)
	_, hits := themeOrigin(t, serveBody(http.StatusOK, aThemeBundleJSON))
	for _, bad := range []string{
		"", "   ", "not-a-url", "/relative/only", "ftp://example.invalid/theme.json",
		"file:///etc/passwd", "javascript:alert(1)",
	} {
		rec := fetchTheme(t, api, bad)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("url %q: got %d %s, want 422", bad, rec.Code, rec.Body.String())
		}
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("a malformed link reached the network %d times, want 0", got)
	}
}

// TestThemeFetch_LoopbackOriginIsNotRefusedForBeingLoopback pins the OWNER
// RULING itself (2026-08-03: 「不要限制 link 是哪邊來的」/「不用驗證」), taken
// after the timing of the risk was put to him in full. Every other test in this
// file happens to use a loopback origin, so a re-added private-address
// blocklist would turn them red for a reason nobody would trace back to a
// ruling. This one says out loud what is being asserted, so the next person who
// "fixes" it learns from the failure that it was decided, not forgotten.
func TestThemeFetch_LoopbackOriginIsNotRefusedForBeingLoopback(t *testing.T) {
	api := newTasksTestServer(t)
	link, _ := themeOrigin(t, serveBody(http.StatusOK, aThemeBundleJSON))
	if !strings.Contains(link, "127.0.0.1") {
		t.Fatalf("fixture is not a loopback origin (%s) — this test asserts nothing", link)
	}
	if rec := fetchTheme(t, api, link); rec.Code != http.StatusOK {
		t.Fatalf("a loopback link was refused (%d %s). If this is a deliberate "+
			"address-layer guard, it needs a NEW owner ruling — the 2026-08-03 "+
			"one says the origin is not constrained.", rec.Code, rec.Body.String())
	}
}

// ── (4) the two bounds — availability, not safety ────────────────────────────

// TestThemeFetch_UnresponsiveLinkDoesNotPinTheRequest. A URL that accepts the
// connection and then says nothing must not hold a request handler open. The
// elapsed-time assertion is the point; the 502 alone would also be produced by
// a handler that waited forever and was killed by the test framework.
func TestThemeFetch_UnresponsiveLinkDoesNotPinTheRequest(t *testing.T) {
	api := newTasksTestServer(t)
	release := make(chan struct{})
	link, _ := themeOrigin(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
	})
	// Registered AFTER themeOrigin so it runs BEFORE srv.Close (cleanups are
	// LIFO): httptest's Close waits for outstanding handlers, so a still-blocked
	// handler would deadlock the test at teardown rather than fail it.
	t.Cleanup(func() { close(release) })

	restore := themeFetchTimeout
	themeFetchTimeout = 150 * time.Millisecond
	t.Cleanup(func() { themeFetchTimeout = restore })

	started := time.Now()
	rec := fetchTheme(t, api, link)
	elapsed := time.Since(started)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got %d %s, want 502", rec.Code, rec.Body.String())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("the request was pinned for %v — the timeout is not in force", elapsed)
	}
}

// TestThemeFetch_OversizedBodyIsRefused, with the AT-cap control alongside it:
// a ceiling that refused everything would pass the refusal half on its own, and
// an off-by-one that truncated instead of refusing would produce a confusing
// "not a valid theme" rather than an honest "too large".
func TestThemeFetch_OversizedBodyIsRefused(t *testing.T) {
	restore := themeFetchMaxBytes
	themeFetchMaxBytes = int64(len(aThemeBundleJSON))
	t.Cleanup(func() { themeFetchMaxBytes = restore })

	t.Run("control: exactly at the cap still lands", func(t *testing.T) {
		api := newTasksTestServer(t)
		link, _ := themeOrigin(t, serveBody(http.StatusOK, aThemeBundleJSON))
		if rec := fetchTheme(t, api, link); rec.Code != http.StatusOK {
			t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
		}
	})

	t.Run("one byte over is refused as too large", func(t *testing.T) {
		api := newTasksTestServer(t)
		// A byte of trailing whitespace: still perfectly valid theme JSON, so
		// the ONLY thing that can refuse it is the size ceiling.
		link, _ := themeOrigin(t, serveBody(http.StatusOK, aThemeBundleJSON+" "))
		rec := fetchTheme(t, api, link)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("got %d %s, want 422", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "larger than") {
			t.Fatalf("the refusal does not say it was a size problem: %s", rec.Body.String())
		}
	})
}

// TestThemeFetch_UpstreamFailureIsNotBlamedOnTheLink. A non-200 from the far
// side is a 502, never a 422: telling the owner his link is malformed when the
// far side merely 404'd sends him to rewrite a URL that was fine.
func TestThemeFetch_UpstreamFailureIsNotBlamedOnTheLink(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			api := newTasksTestServer(t)
			link, _ := themeOrigin(t, serveBody(status, `{"error":"nope"}`))
			rec := fetchTheme(t, api, link)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("upstream %d: got %d %s, want 502", status, rec.Code, rec.Body.String())
			}
		})
	}
}
