// Skeleton generated from server/ocserverd/update_check.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	releaseLatestPagePath = "/pkyosx/OffiCraft/releases/latest"
	releaseFeedPagePath   = "/pkyosx/OffiCraft/releases.atom"
)

// releaseSitePage is one canned answer of the fake github.com.
type releaseSitePage struct {
	status   int
	location string
	body     string
}

var latestIsV123 = map[string]releaseSitePage{
	releaseLatestPagePath: {status: http.StatusFound, location: "https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3"},
}

// newReleaseSiteServer serves GitHub's public release pages from pages; any
// other path fails the test.
func newReleaseSiteServer(t *testing.T, pages map[string]releaseSitePage) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		page, ok := pages[r.URL.RequestURI()]
		if !ok {
			t.Errorf("unexpected release page request %q", r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		if page.location != "" {
			w.Header().Set("Location", page.location)
		}
		w.WriteHeader(page.status)
		_, _ = io.WriteString(w, page.body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// releaseFeedFixture follows the shape of github.com/<repo>/releases.atom:
// newest-created first, no prerelease marker, the tag only in the entry link.
func releaseFeedFixture(tags ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/" xml:lang="en-US">
  <id>tag:github.com,2008:https://github.com/pkyosx/OffiCraft/releases</id>
  <link type="text/html" rel="alternate" href="https://github.com/pkyosx/OffiCraft/releases"/>
  <link type="application/atom+xml" rel="self" href="https://github.com/pkyosx/OffiCraft/releases.atom"/>
  <title>Release notes from OffiCraft</title>
  <updated>2026-10-06T08:00:00Z</updated>
`)
	for i, tag := range tags {
		fmt.Fprintf(&b, `  <entry>
    <id>tag:github.com,2008:Repository/1000/%[1]s</id>
    <updated>2026-10-0%[2]dT08:00:00Z</updated>
    <link rel="alternate" type="text/html" href="https://github.com/pkyosx/OffiCraft/releases/tag/%[1]s"/>
    <title>%[1]s</title>
    <content type="html">&lt;p&gt;Release %[1]s&lt;/p&gt;</content>
    <author>
      <name>pkyosx</name>
    </author>
    <media:thumbnail height="30" width="30" url="https://avatars.githubusercontent.com/u/1?s=60&amp;v=4"/>
  </entry>
`, tag, 6-i)
	}
	b.WriteString("</feed>\n")
	return b.String()
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

func TestReleaseSiteBaseURL(t *testing.T) {
	if got := (&apiServer{releaseSiteBase: "http://example.test"}).releaseSiteBaseURL(); got != "http://example.test" {
		t.Fatalf("releaseSiteBaseURL() with an override = %q, want %q", got, "http://example.test")
	}
	if got := (&apiServer{}).releaseSiteBaseURL(); got != "http://127.0.0.1:1" {
		t.Fatalf("releaseSiteBaseURL() without an override in the test binary = %q, want %q", got, "http://127.0.0.1:1")
	}
	if releaseSiteDefaultBase != "https://github.com" {
		t.Fatalf("shipped release site = %q, want %q", releaseSiteDefaultBase, "https://github.com")
	}
}

func TestReleaseTagFromURL(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "https://github.com/pkyosx/OffiCraft/releases/tag/v0.5.485", want: "v0.5.485"},
		{raw: "/pkyosx/OffiCraft/releases/tag/v1.0.0-rc.2", want: "v1.0.0-rc.2"},
		{raw: "https://github.com/pkyosx/OffiCraft/releases/tag/v1%2F..", want: ""},
		{raw: "https://github.com/pkyosx/OffiCraft/releases/tag/a/b", want: ""},
		{raw: "https://github.com/pkyosx/OffiCraft/releases/tag/", want: ""},
		{raw: "https://github.com/pkyosx/OffiCraft/releases", want: ""},
		{raw: "https://github.com/someone/Other/releases/tag/v9.9.9", want: ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", tc.raw, err)
		}
		if got := releaseTagFromURL(u); got != tc.want {
			t.Errorf("releaseTagFromURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestReleasePageURL(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want string
	}{
		{tag: "v0.5.485", want: "https://github.com/pkyosx/OffiCraft/releases/tag/v0.5.485"},
		{tag: "v1.0.0+build", want: "https://github.com/pkyosx/OffiCraft/releases/tag/v1.0.0+build"},
		{tag: "v1.0.0+build/arm 64", want: "https://github.com/pkyosx/OffiCraft/releases/tag/v1.0.0+build%2Farm%2064"},
	} {
		if got := releasePageURL("https://github.com", tc.tag); got != tc.want {
			t.Errorf("releasePageURL(%q) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

func TestUpdateStatus(t *testing.T) {
	t.Run("a fresh cached newer release answers immediately without another network request", func(t *testing.T) {
		srv, calls := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL, updaterCheckIntervalSecs: 300}
		checked := time.Now()
		api.updateCheck = updateCheckState{
			checkedAt: checked,
			lastOKAt:  checked,
			ok:        true,
			tag:       "v1.2.3",
		}

		available, latest := api.updateStatus()
		if !available || latest == nil || *latest != "v1.2.3" {
			t.Fatalf("updateStatus() = (%v, %v), want (true, %q)", available, latest, "v1.2.3")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("fresh updateStatus made %d network requests, want 0", got)
		}
	})

	t.Run("a cache younger than updater_check_interval_secs is served without a request and an older one starts one refresh", func(t *testing.T) {
		srv, calls := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL, updaterCheckIntervalSecs: 60}
		api.updateCheck = updateCheckState{checkedAt: time.Now().Add(-55 * time.Second), ok: true, tag: "v1.0.0"}

		available, latest := api.updateStatus()
		if !available || latest == nil || *latest != "v1.0.0" {
			t.Fatalf("updateStatus() inside the interval = (%v, %v), want (true, v1.0.0)", available, latest)
		}
		if updateCheckSnapshot(api).fetching {
			t.Fatal("a read inside the interval started a refresh")
		}

		api.updateMu.Lock()
		api.updateCheck.checkedAt = time.Now().Add(-65 * time.Second)
		api.updateMu.Unlock()
		api.updateStatus()
		st := waitForUpdateCheckDone(t, api)
		if !st.ok || st.tag != "v1.2.3" {
			t.Fatalf("refreshed cache = %+v, want a finished v1.2.3 success", st)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("a read past the interval made %d requests, want 1", got)
		}
	})

	t.Run("under GitHub down for an hour with a failed attempt 55s ago, a read inside the interval starts no refresh", func(t *testing.T) {
		srv, _ := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL, updaterCheckIntervalSecs: 300}
		api.updateCheck = updateCheckState{
			checkedAt: time.Now().Add(-55 * time.Second),
			lastOKAt:  time.Now().Add(-time.Hour),
			ok:        true,
			tag:       "v1.0.0",
		}

		api.updateStatus()
		if updateCheckSnapshot(api).fetching {
			t.Fatal("a recent failed attempt did not hold off the next refresh")
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
			w.Header().Set("Location", "https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3")
			w.WriteHeader(http.StatusFound)
		}))
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(release) })
			srv.Close()
		})
		api := &apiServer{releaseSiteBase: srv.URL}

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
		if !st.ok || st.tag != "v1.2.3" || st.fetching {
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
		w.Header().Set("Location", "https://github.com/pkyosx/OffiCraft/releases/tag/v1.2.3")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		srv.Close()
	})
	api := &apiServer{releaseSiteBase: srv.URL}
	api.updateCheck = updateCheckState{
		checkedAt: time.Now(),
		ok:        true,
		tag:       "v1.0.0",
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
	if !st.ok || st.tag != "v1.2.3" || st.fetching {
		t.Fatalf("kicked refresh cache = %+v, want a finished v1.2.3 success", st)
	}
}

func TestRefreshUpdateCheck(t *testing.T) {
	t.Run("a successful fetch writes the complete release result and both freshness stamps", func(t *testing.T) {
		srv, _ := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL}
		api.updateCheck.fetching = true

		api.refreshUpdateCheck(false)
		st := updateCheckSnapshot(api)
		if st.includePre || !st.ok || st.none || st.fetching {
			t.Fatalf("successful refresh cache = %+v, want false/ok/not-none/not-fetching", st)
		}
		if st.checkedAt.IsZero() || st.lastOKAt.IsZero() || !st.checkedAt.Equal(st.lastOKAt) {
			t.Fatalf("successful refresh stamps = checked:%v lastOK:%v, want equal non-zero times", st.checkedAt, st.lastOKAt)
		}
		if st.tag != "v1.2.3" {
			t.Fatalf("successful refresh tag = %q, want %q", st.tag, "v1.2.3")
		}
	})

	t.Run("a failed fetch stamps the attempt but preserves the last successful answer", func(t *testing.T) {
		srv, _ := newReleaseSiteServer(t, map[string]releaseSitePage{
			releaseLatestPagePath: {status: http.StatusInternalServerError, body: "upstream down"},
		})
		oldChecked := time.Unix(100, 0)
		oldOK := time.Unix(200, 0)
		api := &apiServer{releaseSiteBase: srv.URL}
		api.updateCheck = updateCheckState{
			checkedAt: oldChecked,
			lastOKAt:  oldOK,
			ok:        true,
			tag:       "v1.0.0",
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
		if st.tag != "v1.0.0" {
			t.Fatalf("failed refresh tag = %q, want preserved %q", st.tag, "v1.0.0")
		}
	})

	t.Run("a result fetched for an old channel is discarded when the cache channel has moved", func(t *testing.T) {
		srv, _ := newReleaseSiteServer(t, latestIsV123)
		before := updateCheckState{
			includePre: true,
			checkedAt:  time.Unix(300, 0),
			tag:        "v9.0.0",
			fetching:   true,
		}
		api := &apiServer{releaseSiteBase: srv.URL, updateCheck: before}

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
	type result struct {
		tag  string
		none bool
		err  string
	}
	fetch := func(base string, includePre bool) result {
		tag, none, err := fetchLatestOffiCraftRelease(base, includePre)
		r := result{tag: tag, none: none}
		if err != nil {
			r.err = err.Error()
		}
		return r
	}
	for _, tc := range []struct {
		name       string
		includePre bool
		pages      map[string]releaseSitePage
		want       result
	}{
		{
			name:  "stable channel with /releases/latest redirecting to a tag page answers that tag",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusFound, location: "https://github.com/pkyosx/OffiCraft/releases/tag/v0.5.485"}},
			want:  result{tag: "v0.5.485"},
		},
		{
			name:  "stable channel with a relative tag-page redirect answers that tag",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusFound, location: "/pkyosx/OffiCraft/releases/tag/v1.0.0-rc.2"}},
			want:  result{tag: "v1.0.0-rc.2"},
		},
		{
			name:  "stable channel with /releases/latest answering 404 is the successful nothing-published result",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusNotFound, body: "Not Found"}},
			want:  result{none: true},
		},
		{
			name:  "stable channel redirected to the release list is the successful nothing-published result",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusFound, location: "https://github.com/pkyosx/OffiCraft/releases"}},
			want:  result{none: true},
		},
		{
			name:  "stable channel redirected anywhere else is an error",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusFound, location: "https://github.com/login"}},
			want:  result{err: "github redirected to https://github.com/login, not a release page"},
		},
		{
			name:  "stable channel answering 500 is an error",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusInternalServerError, body: "upstream failed"}},
			want:  result{err: "github answered 500"},
		},
		{
			name:  "stable channel answering 200 instead of a redirect is an error",
			pages: map[string]releaseSitePage{releaseLatestPagePath: {status: http.StatusOK, body: "<html></html>"}},
			want:  result{err: "github answered 200"},
		},
		{
			name:       "beta channel answers the semver-greatest feed entry whatever the feed order",
			includePre: true,
			pages: map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusOK,
				body: releaseFeedFixture("v0.5.9", "v0.5.486-rc.1", "v0.5.485", "v0.5.10")}},
			want: result{tag: "v0.5.486-rc.1"},
		},
		{
			name:       "beta channel skips an unorderable tag in favour of the semver-greatest",
			includePre: true,
			pages: map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusOK,
				body: releaseFeedFixture("nightly", "v0.5.3")}},
			want: result{tag: "v0.5.3"},
		},
		{
			name:       "beta channel never lets an unorderable tag listed after a valid one win",
			includePre: true,
			pages: map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusOK,
				body: releaseFeedFixture("v0.5.3", "nightly")}},
			want: result{tag: "v0.5.3"},
		},
		{
			name:       "beta channel with an empty feed is the successful nothing-published result",
			includePre: true,
			pages:      map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusOK, body: releaseFeedFixture()}},
			want:       result{none: true},
		},
		{
			name:       "beta channel with a feed entry that links to no release page is an error",
			includePre: true,
			pages: map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusOK,
				body: `<feed xmlns="http://www.w3.org/2005/Atom"><entry><link rel="alternate" href="https://github.com/pkyosx/OffiCraft/commits"/></entry></feed>`}},
			want: result{err: "releases feed entry 1 links to no release page"},
		},
		{
			name:       "beta channel with a feed that is not XML is an error",
			includePre: true,
			pages:      map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusOK, body: "not xml"}},
			want:       result{err: "the releases feed is unreadable: EOF"},
		},
		{
			name:       "beta channel answering 500 is an error",
			includePre: true,
			pages:      map[string]releaseSitePage{releaseFeedPagePath: {status: http.StatusInternalServerError, body: "upstream failed"}},
			want:       result{err: "github answered 500"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := newReleaseSiteServer(t, tc.pages)
			if got := fetch(srv.URL, tc.includePre); got != tc.want {
				t.Fatalf("fetch = %+v, want %+v", got, tc.want)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("fetch made %d requests, want 1", got)
			}
		})
	}

	t.Run("an unreachable GitHub is an error on either channel", func(t *testing.T) {
		want := result{err: `Get "http://127.0.0.1:1/pkyosx/OffiCraft/releases/latest": dial tcp 127.0.0.1:1: connect: connection refused`}
		if got := fetch("http://127.0.0.1:1", false); got != want {
			t.Fatalf("stable fetch = %+v, want %+v", got, want)
		}
		want = result{err: `Get "http://127.0.0.1:1/pkyosx/OffiCraft/releases.atom": dial tcp 127.0.0.1:1: connect: connection refused`}
		if got := fetch("http://127.0.0.1:1", true); got != want {
			t.Fatalf("beta fetch = %+v, want %+v", got, want)
		}
	})
}

func TestHandleCheckReleaseApiReleaseCheckGet(t *testing.T) {
	t.Run("a well-formed GET /api/release/check answers the complete update body", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv, _ := newReleaseSiteServer(t, latestIsV123)
		api.releaseSiteBase = srv.URL

		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", owner, "")
		if status != http.StatusOK {
			t.Fatalf("GET /api/release/check status = %d, want 200 (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":          "update_available",
			"current_version": "0.0.0",
			"latest_tag":      "v1.2.3",
			"release_url":     srv.URL + "/pkyosx/OffiCraft/releases/tag/v1.2.3",
		})
	})

	t.Run("a GitHub that answers 500 degrades to the unknown body, still a 200", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv, _ := newReleaseSiteServer(t, map[string]releaseSitePage{
			releaseLatestPagePath: {status: http.StatusInternalServerError, body: "upstream failed"},
		})
		api.releaseSiteBase = srv.URL

		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", owner, "")
		if status != http.StatusOK {
			t.Fatalf("GET /api/release/check status = %d, want 200 (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":          "unknown",
			"current_version": "0.0.0",
			"latest_tag":      nil,
			"release_url":     nil,
		})
	})

	t.Run("the beta channel answers the newest feed entry with its release page", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv, _ := newReleaseSiteServer(t, map[string]releaseSitePage{
			releaseFeedPagePath: {status: http.StatusOK, body: releaseFeedFixture("v1.2.3", "v1.3.0-rc.1")},
		})
		api.releaseSiteBase = srv.URL
		if status, data := apiJSON(t, h, http.MethodPatch, "/api/settings", owner, `{"updater_receive_beta":true}`); status != http.StatusOK {
			t.Fatalf("PATCH /api/settings status = %d, want 200 (%v)", status, data)
		}

		status, data := apiJSON(t, h, http.MethodGet, "/api/release/check", owner, "")
		if status != http.StatusOK {
			t.Fatalf("GET /api/release/check status = %d, want 200 (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":          "update_available",
			"current_version": "0.0.0",
			"latest_tag":      "v1.3.0-rc.1",
			"release_url":     srv.URL + "/pkyosx/OffiCraft/releases/tag/v1.3.0-rc.1",
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
		srv, calls := newReleaseSiteServer(t, map[string]releaseSitePage{
			releaseLatestPagePath: {status: http.StatusNotFound, body: "Not Found"},
		})
		api.releaseSiteBase = srv.URL

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
		srv, _ := newReleaseSiteServer(t, map[string]releaseSitePage{
			releaseLatestPagePath: {status: http.StatusNotFound, body: "Not Found"},
		})
		api.releaseSiteBase = srv.URL
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
		srv, calls := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL}
		want := updateCheckState{
			checkedAt: time.Now(),
			lastOKAt:  time.Unix(200, 0),
			ok:        true,
			tag:       "v1.0.0",
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

	t.Run("a cache checked 10s ago is reused by the button without contacting GitHub", func(t *testing.T) {
		srv, calls := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL}
		api.updateCheck = updateCheckState{checkedAt: time.Now().Add(-10 * time.Second), ok: true, tag: "v1.0.0"}

		got := api.syncUpdateCheck()
		if got.tag != "v1.0.0" || calls.Load() != 0 {
			t.Fatalf("10s-old sync = tag %q after %d requests, want v1.0.0 after 0", got.tag, calls.Load())
		}
	})

	t.Run("a cache checked 31s ago is past the button window and is fetched again", func(t *testing.T) {
		srv, calls := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL}
		api.updateCheck = updateCheckState{checkedAt: time.Now().Add(-31 * time.Second), ok: true, tag: "v1.0.0"}

		got := api.syncUpdateCheck()
		if got.tag != "v1.2.3" || calls.Load() != 1 {
			t.Fatalf("31s-old sync = tag %q after %d requests, want v1.2.3 after 1", got.tag, calls.Load())
		}
	})

	t.Run("a stale cache is synchronously replaced by the release result", func(t *testing.T) {
		srv, calls := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL}
		api.updateCheck = updateCheckState{checkedAt: time.Now().Add(-releaseCheckButtonTTL - time.Second)}

		got := api.syncUpdateCheck()
		if !got.ok || got.none || got.fetching || got.tag != "v1.2.3" {
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
		srv, _ := newReleaseSiteServer(t, map[string]releaseSitePage{
			releaseLatestPagePath: {status: http.StatusBadGateway, body: "upstream failed"},
		})
		old := updateCheckState{
			checkedAt: time.Unix(100, 0),
			lastOKAt:  time.Unix(200, 0),
			ok:        true,
			tag:       "v1.0.0",
		}
		api := &apiServer{releaseSiteBase: srv.URL, updateCheck: old}

		got := api.syncUpdateCheck()
		if !reflect.DeepEqual(got, updateCheckState{}) {
			t.Fatalf("failed sync result = %+v, want an unknown result", got)
		}
		shared := updateCheckSnapshot(api)
		if shared.fetching || !shared.ok || !shared.checkedAt.After(old.checkedAt) || !shared.lastOKAt.Equal(old.lastOKAt) || shared.tag != old.tag {
			t.Fatalf("failed sync shared cache = %+v, want old answer with a new attempt stamp", shared)
		}
	})

	t.Run("a channel mismatch does not reuse a fresh cache from the other channel", func(t *testing.T) {
		srv, _ := newReleaseSiteServer(t, latestIsV123)
		api := &apiServer{releaseSiteBase: srv.URL}
		api.updateCheck = updateCheckState{
			includePre: true,
			checkedAt:  time.Now(),
			ok:         true,
			tag:        "v9.0.0",
		}

		got := api.syncUpdateCheck()
		if !got.ok || got.includePre || got.tag != "v1.2.3" {
			t.Fatalf("channel-flip sync result = %+v, want stable-channel v1.2.3", got)
		}
	})
}
