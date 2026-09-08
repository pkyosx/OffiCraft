// Skeleton generated from server/ocserverd/update_check.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const updateCheckReleaseBody = `[{"tag_name":"v1.2.3","html_url":"https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3"}]`

func newUpdateCheckTestServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.URL.RequestURI(); got != "/repos/pkyosx/OffiCraft/releases?per_page=20" {
			t.Errorf("release request URI = %q, want %q", got, "/repos/pkyosx/OffiCraft/releases?per_page=20")
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("release request Accept = %q, want application/vnd.github+json", got)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func updateCheckSnapshot(s *apiServer) updateCheckState {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	return s.updateCheck
}

func waitForUpdateCheckDone(t *testing.T, s *apiServer) updateCheckState {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		st := updateCheckSnapshot(s)
		if !st.fetching && !st.checkedAt.IsZero() {
			return st
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("update check did not finish")
			return updateCheckState{}
		}
	}
}

func TestReceiveBetaEnabled(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	if got := api.receiveBetaEnabled(); got {
		t.Fatal("receiveBetaEnabled() = true on a freshly claimed server, want false")
	}
	status, data := apiJSON(t, h, http.MethodPatch, "/api/settings", owner,
		`{"updater_receive_beta":true}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH /api/settings status = %d, want 200 (%v)", status, data)
	}
	if got := api.receiveBetaEnabled(); !got {
		t.Fatal("receiveBetaEnabled() = false after the live setting changed, want true")
	}
	status, data = apiJSON(t, h, http.MethodGet, "/api/settings", owner, "")
	if status != http.StatusOK {
		t.Fatalf("GET /api/settings status = %d, want 200 (%v)", status, data)
	}
	if got, ok := data["updater_receive_beta"].(bool); !ok || !got {
		t.Fatalf("settings updater_receive_beta = %#v, want true", data["updater_receive_beta"])
	}
}

func TestReleaseAPIBaseURL(t *testing.T) {
	if got := (&apiServer{releaseAPIBase: "http://example.test"}).releaseAPIBaseURL(); got != "http://example.test" {
		t.Fatalf("releaseAPIBaseURL() with an override = %q, want %q", got, "http://example.test")
	}
	if got := (&apiServer{}).releaseAPIBaseURL(); got != releaseAPIDefaultBase {
		t.Fatalf("releaseAPIBaseURL() without an override = %q, want %q", got, releaseAPIDefaultBase)
	}
}

func TestUpdateStatus(t *testing.T) {
	t.Run("a fresh cached newer release answers immediately without another network request", func(t *testing.T) {
		srv, calls := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		api := &apiServer{releaseAPIBase: srv.URL}
		checked := time.Now()
		api.updateCheck = updateCheckState{
			checkedAt: checked,
			lastOKAt:  checked,
			ok:        true,
			rel:       githubRelease{TagName: "v1.2.3", HTMLURL: "https://example.test/v1.2.3"},
		}

		available, latest := api.updateStatus()
		if !available || latest == nil || *latest != "v1.2.3" {
			t.Fatalf("updateStatus() = (%v, %v), want (true, %q)", available, latest, "v1.2.3")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("fresh updateStatus made %d network requests, want 0", got)
		}
	})

	t.Run("a missing cache answers the honest empty result while one background refresh is in flight", func(t *testing.T) {
		var releaseOnce sync.Once
		release := make(chan struct{})
		started := make(chan struct{}, 1)
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			started <- struct{}{}
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, updateCheckReleaseBody)
		}))
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(release) })
			srv.Close()
		})
		api := &apiServer{releaseAPIBase: srv.URL}

		available, latest := api.updateStatus()
		if available || latest != nil {
			t.Fatalf("initial updateStatus() = (%v, %v), want (false, nil)", available, latest)
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("background refresh did not reach the test release server")
		}

		available, latest = api.updateStatus()
		if available || latest != nil {
			t.Fatalf("in-flight updateStatus() = (%v, %v), want (false, nil)", available, latest)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("two stale reads started %d network requests, want 1", got)
		}

		releaseOnce.Do(func() { close(release) })
		st := waitForUpdateCheckDone(t, api)
		if !st.ok || st.rel.TagName != "v1.2.3" || st.fetching {
			t.Fatalf("completed cache = %+v, want a finished v1.2.3 success", st)
		}
		available, latest = api.updateStatus()
		if !available || latest == nil || *latest != "v1.2.3" {
			t.Fatalf("updateStatus() after refresh = (%v, %v), want (true, v1.2.3)", available, latest)
		}
	})
}

func TestKickUpdateCheck(t *testing.T) {
	var releaseOnce sync.Once
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		_, _ = io.WriteString(w, updateCheckReleaseBody)
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		srv.Close()
	})
	api := &apiServer{releaseAPIBase: srv.URL}
	api.updateCheck = updateCheckState{
		checkedAt: time.Now(),
		ok:        true,
		rel:       githubRelease{TagName: "v1.0.0"},
	}

	api.kickUpdateCheck()
	st := updateCheckSnapshot(api)
	if !st.checkedAt.IsZero() || !st.fetching {
		t.Fatalf("kickUpdateCheck cache = %+v, want expired and fetching", st)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("kickUpdateCheck did not start the refresh")
	}

	api.kickUpdateCheck()
	if got := calls.Load(); got != 1 {
		t.Fatalf("a second kick during the in-flight refresh made %d requests, want 1", got)
	}
	releaseOnce.Do(func() { close(release) })
	st = waitForUpdateCheckDone(t, api)
	if !st.ok || st.rel.TagName != "v1.2.3" || st.fetching {
		t.Fatalf("kicked refresh cache = %+v, want a finished v1.2.3 success", st)
	}
}

func TestRefreshUpdateCheck(t *testing.T) {
	t.Run("a successful fetch writes the complete release result and both freshness stamps", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		api := &apiServer{releaseAPIBase: srv.URL}
		api.updateCheck.fetching = true

		api.refreshUpdateCheck(false)
		st := updateCheckSnapshot(api)
		if st.includePre || !st.ok || st.none || st.fetching {
			t.Fatalf("successful refresh cache = %+v, want false/ok/not-none/not-fetching", st)
		}
		if st.checkedAt.IsZero() || st.lastOKAt.IsZero() || !st.checkedAt.Equal(st.lastOKAt) {
			t.Fatalf("successful refresh stamps = checked:%v lastOK:%v, want equal non-zero times", st.checkedAt, st.lastOKAt)
		}
		want := githubRelease{TagName: "v1.2.3", HTMLURL: "https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3"}
		if !reflect.DeepEqual(st.rel, want) {
			t.Fatalf("successful refresh release = %+v, want %+v", st.rel, want)
		}
	})

	t.Run("a failed fetch stamps the attempt but preserves the last successful answer", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusInternalServerError, "upstream down")
		oldChecked := time.Unix(100, 0)
		oldOK := time.Unix(200, 0)
		oldRelease := githubRelease{TagName: "v1.0.0", HTMLURL: "https://example.test/old"}
		api := &apiServer{releaseAPIBase: srv.URL}
		api.updateCheck = updateCheckState{
			checkedAt: oldChecked,
			lastOKAt:  oldOK,
			ok:        true,
			rel:       oldRelease,
			fetching:  true,
		}

		api.refreshUpdateCheck(false)
		st := updateCheckSnapshot(api)
		if st.fetching || !st.ok || st.none || !st.checkedAt.After(oldChecked) {
			t.Fatalf("failed refresh cache = %+v, want finished attempt with old answer retained", st)
		}
		if !st.lastOKAt.Equal(oldOK) {
			t.Fatalf("failed refresh lastOKAt = %v, want %v", st.lastOKAt, oldOK)
		}
		if !reflect.DeepEqual(st.rel, oldRelease) {
			t.Fatalf("failed refresh release = %+v, want preserved %+v", st.rel, oldRelease)
		}
	})

	t.Run("a result fetched for an old channel is discarded when the cache channel has moved", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		before := updateCheckState{
			includePre: true,
			checkedAt:  time.Unix(300, 0),
			rel:        githubRelease{TagName: "v9.0.0"},
			fetching:   true,
		}
		api := &apiServer{releaseAPIBase: srv.URL, updateCheck: before}

		api.refreshUpdateCheck(false)
		if got := updateCheckSnapshot(api); !reflect.DeepEqual(got, before) {
			t.Fatalf("old-channel refresh changed cache:\n got %+v\nwant %+v", got, before)
		}
	})
}

func TestUpdateCheckedOKAt(t *testing.T) {
	api := &apiServer{}
	if got := api.updateCheckedOKAt(); got != nil {
		t.Fatalf("updateCheckedOKAt() before a successful check = %q, want nil", *got)
	}
	api.updateCheck.lastOKAt = time.Date(2026, time.September, 8, 12, 34, 56, 0, time.FixedZone("Taipei", 8*60*60))
	got := api.updateCheckedOKAt()
	if got == nil || *got != "2026-09-08T04:34:56Z" {
		t.Fatalf("updateCheckedOKAt() = %v, want %q", got, "2026-09-08T04:34:56Z")
	}
}

func TestFetchLatestOffiCraftRelease(t *testing.T) {
	t.Run("the greatest admissible semver wins regardless of GitHub creation order", func(t *testing.T) {
		body := `[
{"tag_name":"v0.9.9","html_url":"https://example.test/old"},
{"tag_name":"v2.0.0","html_url":"https://example.test/draft","draft":true},
{"tag_name":"v1.5.0","html_url":"https://example.test/best","assets":[{"name":"ocserverd","browser_download_url":"https://example.test/ocserverd","size":42}]},
{"tag_name":"v1.4.0-rc.1","html_url":"https://example.test/rc","prerelease":true}
]`
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, body)
		got, none, err := fetchLatestOffiCraftRelease(srv.URL, false)
		if err != nil {
			t.Fatalf("fetchLatestOffiCraftRelease: %v", err)
		}
		if none {
			t.Fatal("fetchLatestOffiCraftRelease none = true, want false")
		}
		want := githubRelease{
			TagName: "v1.5.0", HTMLURL: "https://example.test/best",
			Assets: []githubReleaseAsset{{Name: "ocserverd", BrowserDownloadURL: "https://example.test/ocserverd", Size: 42}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("release = %+v, want %+v", got, want)
		}
	})

	t.Run("the prerelease channel admits a newer prerelease while the stable channel excludes it", func(t *testing.T) {
		body := `[{"tag_name":"v1.9.0","html_url":"https://example.test/stable"},{"tag_name":"v2.0.0-rc.1","html_url":"https://example.test/rc","prerelease":true}]`
		srv, calls := newUpdateCheckTestServer(t, http.StatusOK, body)
		stable, none, err := fetchLatestOffiCraftRelease(srv.URL, false)
		if err != nil || none || stable.TagName != "v1.9.0" {
			t.Fatalf("stable fetch = (%+v, %v, %v), want v1.9.0 success", stable, none, err)
		}
		beta, none, err := fetchLatestOffiCraftRelease(srv.URL, true)
		if err != nil || none || beta.TagName != "v2.0.0-rc.1" {
			t.Fatalf("beta fetch = (%+v, %v, %v), want v2.0.0-rc.1 success", beta, none, err)
		}
		if got := calls.Load(); got != 2 {
			t.Fatalf("stable and beta fetches made %d requests, want 2", got)
		}
	})

	t.Run("a list with no admissible release is the successful empty result", func(t *testing.T) {
		body := `[{"tag_name":"v2.0.0","draft":true},{"tag_name":"v2.1.0-rc.1","prerelease":true},{"tag_name":""}]`
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, body)
		got, none, err := fetchLatestOffiCraftRelease(srv.URL, false)
		if err != nil || !none || !reflect.DeepEqual(got, githubRelease{}) {
			t.Fatalf("empty fetch = (%+v, %v, %v), want (zero, true, nil)", got, none, err)
		}
	})

	t.Run("GitHub 404 is the same successful empty result", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusNotFound, `{"message":"Not Found"}`)
		got, none, err := fetchLatestOffiCraftRelease(srv.URL, false)
		if err != nil || !none || !reflect.DeepEqual(got, githubRelease{}) {
			t.Fatalf("404 fetch = (%+v, %v, %v), want (zero, true, nil)", got, none, err)
		}
	})

	t.Run("a non-200 other than 404 is an error", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusBadGateway, "upstream failed")
		got, none, err := fetchLatestOffiCraftRelease(srv.URL, false)
		if !reflect.DeepEqual(got, githubRelease{}) || none || err == nil || err.Error() != "github answered 502" {
			t.Fatalf("502 fetch = (%+v, %v, %v), want (zero, false, github answered 502)", got, none, err)
		}
	})

	t.Run("invalid JSON is an error", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, "not json")
		got, none, err := fetchLatestOffiCraftRelease(srv.URL, false)
		if !reflect.DeepEqual(got, githubRelease{}) || none || err == nil {
			t.Fatalf("invalid JSON fetch = (%+v, %v, %v), want zero/false/error", got, none, err)
		}
	})
}

func TestHandleCheckReleaseApiReleaseCheckGet(t *testing.T) {
	t.Run("a well-formed GET /api/release/check answers the complete update body", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		api.releaseAPIBase = srv.URL

		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", owner, "")
		if status != http.StatusOK {
			t.Fatalf("GET /api/release/check status = %d, want 200 (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":          "update_available",
			"current_version": "0.0.0",
			"latest_tag":      "v1.2.3",
			"release_url":     "https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3",
		})
	})

	t.Run("a GET /api/release/check request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)
		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", "", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("GET /api/release/check without credentials = %d, want 401 (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", agent, "")
		if status != http.StatusForbidden {
			t.Fatalf("GET /api/release/check as agent = %d, want 403 (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("the exact path reaches the release-check handler and answers its distinct body", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv, calls := newUpdateCheckTestServer(t, http.StatusNotFound, `{"message":"Not Found"}`)
		api.releaseAPIBase = srv.URL

		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", owner, "")
		if status != http.StatusOK {
			t.Fatalf("exact release-check route status = %d, want 200 (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":          "up_to_date",
			"current_version": "0.0.0",
			"latest_tag":      nil,
			"release_url":     nil,
		})
		if got := calls.Load(); got != 1 {
			t.Fatalf("exact release-check route made %d release requests, want 1", got)
		}
	})

	t.Run("a malformed body, wrong content type, and oversized body do not change this bodyless GET response", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv, _ := newUpdateCheckTestServer(t, http.StatusNotFound, `{"message":"Not Found"}`)
		api.releaseAPIBase = srv.URL
		req := httptest.NewRequest(http.MethodGet, "/api/release/check", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Authorization", "Bearer "+owner)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/release/check with ignored body = %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != `{"status":"up_to_date","current_version":"0.0.0","latest_tag":null,"release_url":null}` {
			t.Fatalf("body = %q, want the complete up-to-date response", got)
		}
	})
}

func TestSyncUpdateCheck(t *testing.T) {
	t.Run("a button-fresh cache is returned without contacting GitHub", func(t *testing.T) {
		srv, calls := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		api := &apiServer{releaseAPIBase: srv.URL}
		want := updateCheckState{
			checkedAt: time.Now(),
			lastOKAt:  time.Unix(200, 0),
			ok:        true,
			rel:       githubRelease{TagName: "v1.0.0", HTMLURL: "https://example.test/v1.0.0"},
		}
		api.updateCheck = want

		got := api.syncUpdateCheck()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("fresh sync result = %+v, want %+v", got, want)
		}
		if calls.Load() != 0 {
			t.Fatalf("button-fresh sync made %d release requests, want 0", calls.Load())
		}
	})

	t.Run("a stale cache is synchronously replaced by the release result", func(t *testing.T) {
		srv, calls := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		api := &apiServer{releaseAPIBase: srv.URL}
		api.updateCheck = updateCheckState{checkedAt: time.Now().Add(-releaseCheckButtonTTL - time.Second)}

		got := api.syncUpdateCheck()
		if !got.ok || got.none || got.fetching || got.rel.TagName != "v1.2.3" || got.rel.HTMLURL != "https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3" {
			t.Fatalf("stale sync result = %+v, want a finished v1.2.3 success", got)
		}
		if got.checkedAt.IsZero() || !got.lastOKAt.Equal(got.checkedAt) {
			t.Fatalf("stale sync stamps = checked:%v lastOK:%v, want equal non-zero times", got.checkedAt, got.lastOKAt)
		}
		if calls.Load() != 1 {
			t.Fatalf("stale sync made %d release requests, want 1", calls.Load())
		}
	})

	t.Run("a failed synchronous fetch reports unknown while preserving the shared last-known release", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusBadGateway, "upstream failed")
		old := updateCheckState{
			checkedAt: time.Unix(100, 0),
			lastOKAt:  time.Unix(200, 0),
			ok:        true,
			rel:       githubRelease{TagName: "v1.0.0", HTMLURL: "https://example.test/v1.0.0"},
		}
		api := &apiServer{releaseAPIBase: srv.URL, updateCheck: old}

		got := api.syncUpdateCheck()
		if !reflect.DeepEqual(got, updateCheckState{}) {
			t.Fatalf("failed sync result = %+v, want an unknown result", got)
		}
		shared := updateCheckSnapshot(api)
		if shared.fetching || !shared.ok || !shared.checkedAt.After(old.checkedAt) || !shared.lastOKAt.Equal(old.lastOKAt) || !reflect.DeepEqual(shared.rel, old.rel) {
			t.Fatalf("failed sync shared cache = %+v, want old answer with a new attempt stamp", shared)
		}
	})

	t.Run("a channel mismatch does not reuse a fresh cache from the other channel", func(t *testing.T) {
		srv, _ := newUpdateCheckTestServer(t, http.StatusOK, updateCheckReleaseBody)
		api := &apiServer{releaseAPIBase: srv.URL}
		api.updateCheck = updateCheckState{
			includePre: true,
			checkedAt:  time.Now(),
			ok:         true,
			rel:        githubRelease{TagName: "v9.0.0"},
		}

		got := api.syncUpdateCheck()
		if !got.ok || got.includePre || got.rel.TagName != "v1.2.3" {
			t.Fatalf("channel-flip sync result = %+v, want stable-channel v1.2.3", got)
		}
	})
}
