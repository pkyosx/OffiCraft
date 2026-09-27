package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 🔴 No address-layer protection (no host allowlist, no private/loopback refusal, no
// per-hop redirect re-validation) — deliberate, owner ruling 2026-08-03: only the
// format and the fetched JSON are checked. Reversing it needs a new ruling.
//
// The answer is the RAW response text: the cockpit runs it through
// parseImportedBundle, the same function a pasted bundle goes through.

var (
	// Matched to updateCheckTimeout; a var so a test can shrink it.
	themeFetchTimeout = 8 * time.Second
	// Generous on purpose: one legitimate bundle carries a 512 KiB background data
	// URI plus avatars, logo and nav icons.
	themeFetchMaxBytes int64 = 4 << 20
)

func validThemeFetchURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

func (s *apiServer) HandleFetchThemeApiThemeFetchPost(w http.ResponseWriter, r *http.Request) {
	var body ThemeFetchDTO
	if !decodeJSONBodyRequired(w, r, &body, "url") {
		return
	}
	link := strings.TrimSpace(body.Url)
	if !validThemeFetchURL(link) {
		writeError(w, http.StatusUnprocessableEntity,
			"url must be an absolute http:// or https:// link")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, link, nil)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "url could not be used: "+err.Error())
		return
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: themeFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not fetch that link: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway,
			fmt.Sprintf("that link answered %d", resp.StatusCode))
		return
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, themeFetchMaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read that link: "+err.Error())
		return
	}
	if int64(len(raw)) > themeFetchMaxBytes {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("that link's content is larger than the %d-byte limit for a theme",
				themeFetchMaxBytes))
		return
	}

	var bundle ThemeBundleDTO
	if err := json.Unmarshal(raw, &bundle); err != nil {
		writeError(w, http.StatusUnprocessableEntity,
			"that link's content is not a theme bundle: "+err.Error())
		return
	}
	if err := validateThemeBundles([]ThemeBundleDTO{bundle}); err != nil {
		writeError(w, http.StatusUnprocessableEntity,
			"that link's content is not a valid theme: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, themeFetchResultDTO{Content: string(raw)})
}
