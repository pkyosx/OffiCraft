package main

// Admissibility is validateThemeBundle (theme_bundle.go), shared with the
// settings array write — never a second copy here. The two set-level rules
// (the cap, id uniqueness) are answered against the TABLE.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// Returns {id, name} only, never bundles (owner ruling 2026-08-18): bundles
// embed their images (four themes measured 1.59 MB). ListCustomThemes still
// reads whole bundles on purpose: extracting the name in SQL would make
// SQLite's json_extract a second opinion beside Go's decoder, and the two do
// not agree on every input.
func (s *apiServer) HandleListThemesApiThemesGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dal.ListCustomThemes()
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]themeListItemDTO, 0, len(rows))
	for _, row := range rows {
		var item struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(row.Bundle), &item); err != nil {
			internalError(w, fmt.Errorf("stored theme %s is not a decodable bundle: %w",
				strconv.Quote(row.ID), err))
			return
		}
		// Serve the row KEY (what every endpoint addresses the theme by), not
		// the bundle's own id.
		out = append(out, themeListItemDTO{ID: row.ID, Name: item.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

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

// Path id / bundle id agreement is checked in exactly ONE place:
// PutCustomTheme's checkCustomThemeIDMatchesBundle (mapped to 422 below). A
// second check here was proven decorative by a mutant — the stored bytes are
// marshalled from this DTO.
func (s *apiServer) HandlePutThemeApiThemesThemeIdPut(w http.ResponseWriter, r *http.Request, themeID string) {
	var body ThemeBundleDTO
	if !decodeJSONBodyStrict(w, r, &body, "id", "name", "colors") {
		return
	}
	// Do not prune unknown wording codes before validation: validateWording
	// caps the RAW entry count and prunes after, so pruning first would let
	// unrecognised entries bypass the cap.
	if err := validateThemeBundle(body, "theme "+strconv.Quote(themeID), nil); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// created reports whether themeID is new, or refuses a new one over the cap.
	created := func() (bool, error) {
		existing, err := s.dal.GetCustomTheme(themeID)
		if err != nil || existing != nil {
			return false, err
		}
		n, err := s.dal.CountCustomThemes()
		if err != nil {
			return false, err
		}
		if n >= maxCustomThemes {
			return false, refuseInTx(http.StatusUnprocessableEntity,
				"at most "+strconv.Itoa(maxCustomThemes)+" custom themes may be saved — delete one first")
		}
		return true, nil
	}
	if _, err := created(); err != nil {
		writeTxError(w, err)
		return
	}

	raw, err := marshalThemeBundle(body)
	if err != nil {
		internalError(w, err)
		return
	}
	var isNew bool
	var stored *CustomTheme
	err = s.dal.inTx(func(*writeTx) error {
		var err error
		if isNew, err = created(); err != nil {
			return err
		}
		if err := s.dal.PutCustomTheme(themeID, raw); err != nil {
			return err
		}
		stored, err = s.dal.GetCustomTheme(themeID)
		if err == nil && stored == nil {
			err = fmt.Errorf("theme %s vanished between the write and the read-back", strconv.Quote(themeID))
		}
		return err
	})
	if errors.Is(err, ErrCustomThemeIDBlank) ||
		errors.Is(err, ErrCustomThemeBundleNotJSON) ||
		errors.Is(err, ErrCustomThemeIDMismatch) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeTxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, themeWriteReceiptDTO{
		ID:        themeID,
		Created:   isNew,
		OrderIdx:  stored.OrderIdx,
		UpdatedAt: stored.UpdatedAt,
	})
}

func (s *apiServer) HandleDeleteThemeApiThemesThemeIdDelete(w http.ResponseWriter, r *http.Request, themeID string) {
	unlockMu := s.settingsWriteMu.Acquire()
	defer unlockMu()
	reset := s.displayTheme == themeID
	err := s.dal.inTx(func(tx *writeTx) error {
		deleted, err := deleteCustomThemeOn(tx, themeID)
		if err != nil {
			return err
		}
		if !deleted {
			return refuseInTx(http.StatusNotFound, "theme '"+themeID+"' not found")
		}
		if reset {
			return putSettingOn(tx, settingDisplayTheme, "")
		}
		return nil
	})
	if err != nil {
		unlockMu()
		writeTxError(w, err)
		return
	}
	if reset {
		s.applySettings(func() { s.displayTheme = "" })
	}
	unlockMu()

	writeJSON(w, http.StatusOK, themeDeleteResultDTO{
		ID: themeID, Deleted: true, DisplayThemeReset: reset,
	})
}

// decodeStoredThemeBundle prunes wording codes this build does not know (a
// theme exported from a build with more message keys must not serve dead
// codes; losing this is silent). Not in the DAL on purpose: the DAL returns
// the ORIGINAL bytes, which is what makes the byte-for-byte migration claim
// provable.
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

func marshalThemeBundle(b ThemeBundleDTO) (string, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

const displayThemeRefusal = `display_theme must be "", office, office-light, or an existing custom theme id`

// displayThemeExistsOn asks the TABLE; never keep a copy of the id set
// elsewhere. The patch asks again inside the transaction that sets the theme,
// under settingsWriteMu, which DELETE /api/themes/{id} also holds while it deletes.
func displayThemeExistsOn(q sqlRowQuerier, theme string) (bool, error) {
	if theme == "" || displayThemeAllowed[theme] {
		return true, nil
	}
	row, err := getCustomThemeOn(q, theme)
	if err != nil {
		return false, err
	}
	return row != nil, nil
}
