package main

// api_themes.go — T-83ef, the per-theme endpoints the custom_theme table was
// created to make possible.
//
// WHAT THIS REPLACES, because it explains every choice below. Custom themes used
// to live as ONE json array inside ONE settings value, which meant there was no
// per-theme write at all: "save this theme" was spelled "re-send every theme,
// including every embedded image". Here the unit is a row and the wire verb acts
// on one id. `display.custom_themes` is gone from the settings face in this same
// package, so this table is now the only truth on the wire — see the header of
// dal_custom_themes.go for the retire-vs-double-write question and which way it
// was answered.
//
// 🔴 THE VALIDATOR IS SHARED, NOT REIMPLEMENTED. Every rule that decided whether
// a bundle was admissible through `PATCH /api/settings` decides it here too, via
// validateThemeBundle — the single-bundle half of the same function the array
// write uses (theme_bundle.go). Writing a second opinion about what a legal
// theme is would be a second thing to drift; the two set-level rules that a
// slice could answer and a single write cannot (the cap, and id uniqueness) are
// answered against the TABLE below instead.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// HandleListThemesApiThemesGet answers GET /api/themes — one line per saved
// theme, in the owner's list order, carrying id and name and nothing else.
//
// 🔴 IT DOES NOT RETURN THE BUNDLES, AND THAT IS THE ENDPOINT'S REASON FOR
// EXISTING IN THIS SHAPE (owner ruling 2026-08-18: the list needs the title and
// the little the cockpit shows, nothing more). A theme carries its images
// embedded — on the install this ticket moved, four themes come to 1.59 MB and
// one of them is 953 KB by itself — so a list of whole bundles is the same
// several-hundred-kilobyte answer that made GET /api/settings unusable. Serving
// it again from a new path would have relocated the problem rather than fixed
// it. id and name are exactly what ThemeSettings' list and the profile picker
// render; applying, editing and exporting are all about ONE theme.
//
// ⚠️ HONEST LIMIT ON HOW MUCH THIS ACTUALLY SAVES. The RESPONSE is small; the
// READ is not. ListCustomThemes still selects the bundle column, so those bytes
// still come out of SQLite and through this process — what is avoided is
// decoding them into maps and sending them over the wire. Making the read itself
// cheap needs SQL-side extraction of the name, and that was NOT done on purpose:
// it would make SQLITE's json_extract a second opinion about what a bundle's
// name is, alongside Go's decoder, and the two do not agree on every input both
// accept. This ticket has already paid for that lesson twice
// (checkCustomThemeIDMatchesBundle). One decoder, larger read.
func (s *apiServer) HandleListThemesApiThemesGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dal.ListCustomThemes()
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]themeListItemDTO, 0, len(rows))
	for _, row := range rows {
		// Decoded into a struct carrying only the two fields served: the images
		// are in the raw text either way, but nothing builds a map of them.
		var item struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(row.Bundle), &item); err != nil {
			internalError(w, fmt.Errorf("stored theme %s is not a decodable bundle: %w",
				strconv.Quote(row.ID), err))
			return
		}
		// row.ID is the KEY the theme is filed under and is what every other
		// endpoint addresses it by; the bundle's own id is required to equal it on
		// every write. Serving the key rather than the decoded field means a row
		// that somehow disagreed still lists under the id that actually works.
		out = append(out, themeListItemDTO{ID: row.ID, Name: item.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

// HandleGetThemeApiThemesThemeIdGet answers GET /api/themes/{theme_id} — the
// per-item read that makes "edit one theme" possible without pulling the set.
func (s *apiServer) HandleGetThemeApiThemesThemeIdGet(w http.ResponseWriter, r *http.Request, themeID string) {
	row, err := s.dal.GetCustomTheme(themeID)
	if err != nil {
		internalError(w, err)
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "theme '"+themeID+"' not found")
		return
	}
	b, err := decodeStoredThemeBundle(*row)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// HandlePutThemeApiThemesThemeIdPut answers PUT /api/themes/{theme_id} — create
// or replace ONE theme. This is the write the whole split exists to express.
//
// 🔴 THE PATH ID IS THE KEY AND THE BUNDLE MUST AGREE WITH IT — AND THAT IS
// CHECKED IN EXACTLY ONE PLACE, WHICH IS NOT HERE. The refusal comes from
// PutCustomTheme's checkCustomThemeIDMatchesBundle, which asks SQLITE what the
// stored bytes say the id is; this handler maps its named error to a 422.
//
// An earlier draft of this function ALSO compared body.Id to themeID up front,
// with a comment explaining that the two checks answer different questions
// (decoded DTO vs stored bytes) and so both were needed. A mutant disproved it:
// deleting the check here left every assertion green, and only deleting the DAL
// check turned the test red. It was decorative, and on this path it could not be
// anything else — the bytes handed to the DAL are marshalled FROM this DTO, so
// the id SQLite reads is by construction the id Go decoded. The disagreements
// that function was written for (duplicate keys, lone surrogates, numeric ids)
// live on paths where the caller's raw text is stored, not this one.
//
// So: one authority, not two opinions. "Silently filed under the other id" — the
// outcome worth preventing — is prevented by the check that a mutant can kill.
func (s *apiServer) HandlePutThemeApiThemesThemeIdPut(w http.ResponseWriter, r *http.Request, themeID string) {
	var body ThemeBundleDTO
	if !decodeJSONBodyStrict(w, r, &body, "id", "name", "colors") {
		return
	}
	// The wording overlay's unknown-code PRUNE is not done here: it lives inside
	// validateWording, which the call below reaches, and it must stay there.
	//
	// 🔴 THIS USED TO PRUNE FIRST AND THAT WAS A HOLE — caught by the wording
	// matrix ported from the settings test, which is the whole reason those
	// assertions were moved rather than dropped. validateWording bounds a
	// language's overlay by its RAW submitted entry count and only then drops the
	// unrecognised codes. Pruning before it runs meant a caller could send any
	// number of entries as long as they were unrecognised, and the cap — whose
	// entire job is to bound what an untrusted caller can submit — passed a map
	// that had already been emptied for it.
	// Normalize BEFORE validating and storing. A legacy singleton
	// avatars.member becomes a one-image pool, a pool written as bare data-URI
	// strings is lifted to items, and every item is stamped with the identity
	// DERIVED from its bytes. Doing it here means the STORED bytes already
	// carry the ids a member's selection points at — the read path never has
	// to invent one, and an id the caller sent is overwritten rather than
	// trusted.
	if err := normalizeThemeAvatarPools(&body, "theme "+strconv.Quote(themeID)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	assignThemeIconIDs(body.AvatarPools)
	if err := validateThemeBundle(body, "theme "+strconv.Quote(themeID), nil); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// The two set-level rules the single-bundle validator deliberately does not
	// answer. Both are asked of the TABLE, and both are asked BEFORE the write.
	existing, err := s.dal.GetCustomTheme(themeID)
	if err != nil {
		internalError(w, err)
		return
	}
	if existing == nil {
		// The cap bounds how many themes are KEPT, so it constrains creates and
		// not replaces — re-saving one of N themes when N is already the cap has
		// to keep working, or an owner at the limit could no longer edit.
		n, err := s.dal.CountCustomThemes()
		if err != nil {
			internalError(w, err)
			return
		}
		if n >= maxCustomThemes {
			writeError(w, http.StatusUnprocessableEntity,
				"at most "+strconv.Itoa(maxCustomThemes)+" custom themes may be saved — delete one first")
			return
		}
		// ⚠️ COUNT-THEN-WRITE, not atomic. Two creates racing here can both see
		// n == cap-1 and both land, leaving cap+1 rows. Known and accepted, not
		// missed: the cap bounds how much one owner may keep, it is not a
		// security boundary, and overshooting it by the number of concurrent
		// writers costs nothing (the next create is refused normally). Closing
		// it means counting inside the same transaction as the insert, which is
		// a change to the DAL's write seam and wants its own decision — see the
		// same note on displayThemeExists below, which is the sharper half of
		// this pair.
	}

	raw, err := marshalThemeBundle(body)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.PutCustomTheme(themeID, raw); err != nil {
		// The DAL's three named refusals are the caller's fault and say which
		// field is wrong; errors.Is, never a string match on a database message
		// whose wording nobody has promised to keep stable.
		if errors.Is(err, ErrCustomThemeIDBlank) ||
			errors.Is(err, ErrCustomThemeBundleNotJSON) ||
			errors.Is(err, ErrCustomThemeIDMismatch) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		internalError(w, err)
		return
	}

	stored, err := s.dal.GetCustomTheme(themeID)
	if err != nil {
		internalError(w, err)
		return
	}
	if stored == nil {
		// The row was written and is already gone: a concurrent delete. Reading
		// it back rather than reporting the values we intended to write is what
		// makes that visible instead of inventing a receipt for a theme that no
		// longer exists.
		internalError(w, fmt.Errorf("theme %s vanished between the write and the read-back", strconv.Quote(themeID)))
		return
	}
	// A theme write can remove a pool image, which leaves every member that
	// chose it pointing at nothing. Prune here rather than at render time: a
	// theme id that is later reused would otherwise resurrect the old
	// selections. This endpoint is the ONLY path that edits a theme, so it is
	// the only place that can see the change.
	//
	// BEST-EFFORT: the theme is already stored and there is no transaction to
	// roll back, so a 500 here would report "the write failed" for a write that
	// succeeded. A leftover row is invisible — memberAvatarIconID resolves
	// against the live pool and sends null for an id it cannot find.
	if err := s.dal.PruneMemberThemeAvatars(s.themeIconIDs()); err != nil {
		taskLog("theme %s: avatar selection prune failed: %v", themeID, err)
	}
	s.invalidateAvatarSelections()
	writeJSON(w, http.StatusOK, themeWriteReceiptDTO{
		ID:        themeID,
		Created:   existing == nil,
		OrderIdx:  stored.OrderIdx,
		UpdatedAt: stored.UpdatedAt,
	})
}

// HandleDeleteThemeApiThemesThemeIdDelete answers DELETE /api/themes/{theme_id}.
//
// 🔴 IT CARRIES THE COUPLING THE SETTINGS WRITE USED TO CARRY. Deleting the
// ACTIVE theme leaves display.theme pointing at nothing, so it is reset to ""
// here, in the same request, and the receipt says whether that happened. The old
// whole-array write did this because it could see both facts at once; now only
// this endpoint can, so refusing to do it would leave the cockpit painting a
// theme that no longer exists with nothing telling it so.
func (s *apiServer) HandleDeleteThemeApiThemesThemeIdDelete(w http.ResponseWriter, r *http.Request, themeID string) {
	deleted, err := s.dal.DeleteCustomTheme(themeID)
	if err != nil {
		internalError(w, err)
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "theme '"+themeID+"' not found")
		return
	}

	// Reset AFTER the row is gone, so the state the reset is derived from is the
	// state that will be served. Doing it first would leave a window in which
	// display.theme is "" while the theme still exists.
	reset := false
	s.settingsMu.Lock()
	if s.displayTheme == themeID {
		if err := s.dal.PutSetting(settingDisplayTheme, ""); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.displayTheme = ""
		reset = true
	}
	s.settingsMu.Unlock()

	// Deleting a theme leaves every selection made inside it dangling. Prune
	// here rather than at render time: a theme id that is later reused would
	// otherwise resurrect the old selections. This endpoint is the ONLY path
	// that deletes a theme.
	//
	// BEST-EFFORT: the row is already gone and there is no transaction to roll
	// back, so a 500 here would report "the delete failed" for a delete that
	// succeeded. A leftover association is invisible — memberAvatarIconID
	// resolves against the live pool and sends null for an id it cannot find.
	if err := s.dal.PruneMemberThemeAvatars(s.themeIconIDs()); err != nil {
		taskLog("theme %s: avatar selection prune failed: %v", themeID, err)
	}
	s.invalidateAvatarSelections()
	writeJSON(w, http.StatusOK, themeDeleteResultDTO{
		ID: themeID, Deleted: true, DisplayThemeReset: reset,
	})
}

// decodeStoredThemeBundle turns one stored row back into the wire DTO.
//
// 🔴 THIS IS WHERE THE READ-PATH WORDING PRUNE LIVES NOW, and it had to move
// somewhere the moment themes stopped being loaded through settings. The old
// home was the settings loader, which ran dropUnknownWordingCodes over every
// bundle as it read them: a theme exported from a build that knew message keys
// this build does not must not serve those dead codes back. Nothing else on this
// path would have done it, and losing it is silent — the codes simply reappear.
//
// ⚠️ IT DELIBERATELY DOES NOT LIVE IN THE DAL. That layer stores and returns the
// bundle's ORIGINAL BYTES, which is what makes "the migration moved these
// themes byte for byte" a mechanically provable claim; decoding and re-encoding
// underneath that guarantee would quietly end it. Pruning is a wire concern, so
// it happens at the wire.
func decodeStoredThemeBundle(row CustomTheme) (ThemeBundleDTO, error) {
	var b ThemeBundleDTO
	if err := json.Unmarshal([]byte(row.Bundle), &b); err != nil {
		return ThemeBundleDTO{}, fmt.Errorf("stored theme %s is not a decodable bundle: %w",
			strconv.Quote(row.ID), err)
	}
	if w := b.Wording; w != nil {
		dropUnknownWordingCodes(*w, "stored theme "+strconv.Quote(row.ID))
	}
	return b, nil
}

// marshalThemeBundle renders a validated bundle to the JSON text the table
// stores. It is a named seam rather than an inline json.Marshal so that the one
// place deciding what bytes a theme is stored as stays findable.
func marshalThemeBundle(b ThemeBundleDTO) (string, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// displayThemeExists reports whether a proposed display.theme value names
// something that can actually be applied: "" (unset), a built-in, or a custom
// theme that HAS A ROW right now.
//
// 🔴 IT ASKS THE TABLE, and that is the whole reason it exists rather than the
// settings handler keeping its own id set. Before T-83ef the vocabulary came
// from the bundle array that arrived in the same request, so "which ids are
// legal" was answerable from the request alone. It is not any more — the themes
// live in their own table, written by their own endpoints, possibly by a
// different caller a moment ago. Any copy of that id set held anywhere else
// would be a snapshot with no one keeping it fresh, which is exactly the class
// of bug this ticket has been paying for.
//
// ⚠️ CHECK-THEN-SET. The SHAPE is real and it is new: this answer is true when
// it is given, the caller then writes display_theme under settingsMu, and this
// lookup sits outside that lock — so a DELETE of the same theme in between
// would leave display_theme naming a theme with no row. The shape did not exist
// before T-83ef, when the vocabulary and the selection arrived in ONE request
// under one lock.
//
// 🔴 REACHABILITY IS UNPROVEN, and that is stated deliberately rather than left
// to read as a live hazard. A probe ran the two requests concurrently 300 times
// and produced the dangling state ZERO times; every observed interleaving was
// "the delete lands first, the patch is then refused 422". One plausible reason
// is that the write pool is capped at one connection and serialises the
// dangerous order away — but that is a GUESS, it was not proven, and it must
// not be quoted as if it were. So: the shape exists, nobody has reached it, and
// neither "it happens" nor "it cannot" is claimed here.
//
// (The sibling count-then-write above IS reachable — the same probe reproduced
// cap+1 rows. It is tracked separately as T-f49e; this one is not, on the
// ruling that acting on an unreached race would trade a certain risk for an
// unknown gain.)
//
// It is left open on purpose, and here is what actually absorbs it: the cockpit
// treats a display_theme it cannot find in the list as "not selectable" and
// falls back to the built-in on the next reconcile (i18n/index.tsx), which is
// the T-1500 rule and is pinned by a guard that survives even when the stale
// paint record cannot be removed. So the visible outcome is the built-in theme,
// not a broken screen.
//
// What would NOT be absorbed, and is the reason this note exists rather than a
// silent shrug: any future reader that treats display_theme as a guaranteed
// foreign key — a join, a NOT NULL reference, a migration that assumes every
// display_theme has a row. Closing the window means taking settingsMu across
// the lookup and the write, or a real transaction spanning both resources.
// That is a locking decision, not an implementation detail, and this ticket's
// scope was ruled to be the split alone.
func (s *apiServer) displayThemeExists(theme string) (bool, error) {
	if theme == "" || displayThemeAllowed[theme] {
		return true, nil
	}
	row, err := s.dal.GetCustomTheme(theme)
	if err != nil {
		return false, err
	}
	return row != nil, nil
}
