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

// ── T-29c7 主題匯入吃連結 ────────────────────────────────────────────────────
//
// The cockpit's theme import box only ever took text a human had in hand:
// pasted JSON, or a .json file picked from the local disk. That is a real
// channel problem, not a convenience gap — a theme carrying a background image
// runs to hundreds of thousands of characters (T-72da raised the background cap
// to 512 KiB), while a chat message body is hard-capped at 4000. So an agent
// could BUILD a finished theme and still have no way at all to hand it over;
// themes were something the owner could only ever make himself. One link closes
// that: the agent uploads the bundle and mints a share link, the owner pastes
// the link, the server reads it back.
//
// The shape is deliberately thin. This endpoint fetches and answers with the
// RAW response text; the cockpit then runs it through parseImportedBundle — the
// exact function a pasted or file-picked bundle already goes through. A
// link-imported theme therefore cannot be validated differently from a pasted
// one, because there is only one validator on that path.
//
// 🔴 NO ADDRESS-LAYER PROTECTION HERE — DELIBERATE, owner ruling 2026-08-03.
// There is no host allowlist, no refusal of private / loopback / link-local
// addresses, and no per-hop redirect re-validation. That is not an oversight
// and it is not something to quietly "fix" while passing by. The timing of the
// risk was put to the owner explicitly BEFORE he ruled — that checking the
// FORMAT cannot cover it, because the fetch happens first and the risk is
// complete at the moment the server dials, before any byte of content exists to
// inspect. His answer, verbatim: 「不要限制 link 是哪邊來的 但是可以檢查
// format?」/「不用驗證」/「只要管拿到的 json 符合預期」. Reversing this needs a
// NEW ruling, not a refactor.
//
// What IS bounded is the CALL, and the reason is availability rather than
// safety: a URL that never answers, or answers forever, would otherwise pin a
// request handler for as long as it liked. Hence a timeout and a hard read
// ceiling — both of which are indifferent to WHERE the link points, so neither
// smuggles the refused address check back in through the side door.

var (
	// themeFetchTimeout bounds one outbound theme fetch end to end (dial +
	// response). Matched to updateCheckTimeout — the other place this server
	// reaches out to something it does not control. A var, not a const, so a
	// test can shrink it without waiting eight real seconds.
	themeFetchTimeout = 8 * time.Second
	// themeFetchMaxBytes caps how much of the response body is read. A generous
	// ceiling on purpose: one legitimate bundle can legitimately be large (a
	// 512 KiB background data URI plus avatars, logo and nav icons), so a tight
	// cap would refuse exactly the themes this endpoint exists to carry. It is
	// here to stop an endless body, not to second-guess a real theme.
	themeFetchMaxBytes int64 = 4 << 20
)

// validThemeFetchURL is the FORMAT check the owner did allow: the address must
// parse, must be absolute, and must be http/https. It says nothing whatsoever
// about WHERE the address points — see the note above.
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

// HandleFetchThemeApiThemeFetchPost reads a theme bundle from a link.
//
// The answer is the raw response text, not a re-serialised bundle. That is the
// whole reuse argument: whatever comes back goes into the SAME
// parseImportedBundle the paste box uses, so the two import paths cannot drift
// into accepting different things. Re-encoding here would create a second
// serialisation of a theme and a second place for it to be wrong.
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
		// 502, not 422: the link itself was well formed — whatever is on the
		// other end is what failed. Telling the owner "your link is invalid"
		// when the far side merely timed out would send him to rewrite a URL
		// that was fine.
		writeError(w, http.StatusBadGateway, "could not fetch that link: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway,
			fmt.Sprintf("that link answered %d", resp.StatusCode))
		return
	}

	// Read one byte past the ceiling: that is how an oversized body is
	// DISTINGUISHED from one that happens to end exactly at the cap. Reading
	// only up to the cap would silently hand back a truncated bundle, which
	// then fails validation for a reason that has nothing to do with what is
	// wrong.
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

	// 「只要管拿到的 json 符合預期」 — the one thing the owner DID ask for. The
	// bundle goes through validateThemeBundles, the very same validator that
	// guards the theme write itself (PATCH /api/settings' custom_themes array
	// until T-83ef, PUT /api/themes/{theme_id} since); a link that points at
	// something which is not a theme is refused HERE, naming what is wrong,
	// rather than turning into a puzzling failure two layers up the UI.
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
