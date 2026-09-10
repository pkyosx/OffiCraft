// Skeleton generated from server/ocserverd/api_themes.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// apiThemeList drives GET /api/themes and asserts the WHOLE array — the route
// answers a bare JSON array, which apiJSON's object decode cannot carry.
func apiThemeList(t *testing.T, h http.Handler, credential string, want ...map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/themes", credential, "")
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	rows := make([]any, len(want))
	for i := range want {
		rows[i] = want[i]
	}
	apiWantValue(t, "body", got, rows)
}

// apiPutTheme files one theme through the real write route and fails the test
// unless it landed.
func apiPutTheme(t *testing.T, h http.Handler, credential, id, body string) {
	t.Helper()
	if status, data := apiJSON(t, h, "PUT", "/api/themes/"+id, credential, body); status != 200 {
		t.Fatalf("put theme %s: %d %v", id, status, data)
	}
}

// apiThemeAbsent asserts nothing is filed under id, read back through the route
// that would serve it.
func apiThemeAbsent(t *testing.T, h http.Handler, credential, id string) {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/themes/"+id, credential, "")
	if status != 404 {
		t.Fatalf("theme %s should not exist, got %d (%v)", id, status, data)
	}
}

func apiDisplayTheme(t *testing.T, h http.Handler, credential string) string {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/settings", credential, "")
	if status != 200 {
		t.Fatalf("get settings: %d %v", status, data)
	}
	theme, _ := data["display_theme"].(string)
	return theme
}

func TestHandleListThemesApiThemesGet(t *testing.T) {
	t.Run("an office that has saved no theme answers an empty array", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiThemeList(t, h, owner)
		dashboard.wantFrames()
	})

	t.Run("every saved theme lists as id and name only, in the order they were filed, never the bundle", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"},"fonts":{"--font-sans":"\"Yuanti TC\", \"Yuanti SC\", sans-serif"}}`)
		apiPutTheme(t, h, owner, "dawn", `{"id":"dawn","name":"Dawn","colors":{"--color-bg":"#fffdf7","--color-text":"rgb(20, 20, 20)"}}`)
		dashboard := apiTestListen(t, api, "")

		apiThemeList(t, h, owner,
			map[string]any{"id": "dusk", "name": "Dusk"},
			map[string]any{"id": "dawn", "name": "Dawn"},
		)
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/themes", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/themes", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("an ignored request body does not change the theme list", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/api/themes", owner, "{{{")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{})
	})
}

func TestHandleGetThemeApiThemesThemeIdGet(t *testing.T) {
	t.Run("the stored bundle comes back whole, carrying exactly the fields it was filed with", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418","--color-text":"rgba(255, 255, 255, 0.9)"},"fonts":{"--font-sans":"\"Yuanti TC\", \"Yuanti SC\", sans-serif"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/themes/dusk", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":   "dusk",
			"name": "Dusk",
			"colors": map[string]any{
				"--color-bg":   "#101418",
				"--color-text": "rgba(255, 255, 255, 0.9)",
			},
			"fonts": map[string]any{"--font-sans": `"Yuanti TC", "Yuanti SC", sans-serif`},
		})
		dashboard.wantFrames()
	})

	t.Run("the id in the path picks the row, and the other saved theme is not what comes back", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		apiPutTheme(t, h, owner, "dawn", `{"id":"dawn","name":"Dawn","colors":{"--color-bg":"#fffdf7"}}`)

		status, data := apiJSON(t, h, "GET", "/api/themes/dawn", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":     "dawn",
			"name":   "Dawn",
			"colors": map[string]any{"--color-bg": "#fffdf7"},
		})
	})

	t.Run("a wording code this build no longer knows is pruned out of the answer while the recognised one survives", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"},"wording":{"zh":{"nav.tasks":"活兒","no.such.code":"gone"}}}`)

		status, data := apiJSON(t, h, "GET", "/api/themes/dusk", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":      "dusk",
			"name":    "Dusk",
			"colors":  map[string]any{"--color-bg": "#101418"},
			"wording": map[string]any{"zh": map[string]any{"nav.tasks": "活兒"}},
		})
	})

	t.Run("an id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/themes/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "theme 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/themes/dusk", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/themes/dusk", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("an ignored request body does not change the selected theme", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)

		status, data := apiJSON(t, h, "GET", "/api/themes/dusk", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":     "dusk",
			"name":   "Dusk",
			"colors": map[string]any{"--color-bg": "#101418"},
		})
	})
}

func TestHandlePutThemeApiThemesThemeIdPut(t *testing.T) {
	t.Run("a first write files the theme and the receipt says it was created", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dusk", owner,
			`{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "dusk", "created": true, "order_idx": 0, "updated_at": apiAnyNumber,
		})
		dashboard.wantFrames()

		_, stored := apiJSON(t, h, "GET", "/api/themes/dusk", owner, "")
		apiWantBody(t, stored, map[string]any{
			"id": "dusk", "name": "Dusk", "colors": map[string]any{"--color-bg": "#101418"},
		})
	})

	t.Run("a second write replaces the bundle in place, keeps the theme's list position and says it was not created", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		apiPutTheme(t, h, owner, "dawn", `{"id":"dawn","name":"Dawn","colors":{"--color-bg":"#fffdf7"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dawn", owner,
			`{"id":"dawn","name":"Dawn II","colors":{"--color-bg":"#ffffff"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "dawn", "created": false, "order_idx": 1, "updated_at": apiAnyNumber,
		})
		dashboard.wantFrames()

		_, stored := apiJSON(t, h, "GET", "/api/themes/dawn", owner, "")
		apiWantBody(t, stored, map[string]any{
			"id": "dawn", "name": "Dawn II", "colors": map[string]any{"--color-bg": "#ffffff"},
		})
		apiThemeList(t, h, owner,
			map[string]any{"id": "dusk", "name": "Dusk"},
			map[string]any{"id": "dawn", "name": "Dawn II"},
		)
	})

	t.Run("a bundle whose own id disagrees with the path is refused 422 and nothing is filed under either id", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dawn", owner,
			`{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`custom theme: the bundle's own id does not match the id it is being filed under: `+
				`bundle says "dusk", filed under "dawn"`)
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dawn")
		apiThemeAbsent(t, h, owner, "dusk")
		apiThemeList(t, h, owner)
	})

	t.Run("a colour value outside the concrete-colour allowlist is refused 422 naming the token, and nothing is filed", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dusk", owner,
			`{"id":"dusk","name":"Dusk","colors":{"--color-bg":"url(x)"}}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`theme "dusk": "--color-bg" has an invalid colour value "url(x)" — `+
				`only concrete hex / rgb() / rgba() / hsl() / hsla() / transparent are accepted`)
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dusk")
	})

	t.Run("an id that is not a legal slug is refused 422 and nothing is filed", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/Dusk", owner,
			`{"id":"Dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`theme "Dusk": id must match ^[a-z0-9][a-z0-9-]{1,63}$ (got "Dusk")`)
		dashboard.wantFrames()
		apiThemeList(t, h, owner)
	})

	t.Run("a built-in theme's id cannot be claimed by a custom bundle", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/office", owner,
			`{"id":"office","name":"Office","colors":{"--color-bg":"#101418"}}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`theme "office": id "office" is reserved for a built-in theme`)
		dashboard.wantFrames()
		apiThemeList(t, h, owner)
	})

	t.Run("a body with no colours at all is refused 422 naming the missing field", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dusk", owner, `{"id":"dusk","name":"Dusk"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: colors")
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dusk")
	})

	t.Run("a PUT /api/themes/{theme_id} request the wire layer rejects (a malformed JSON body) answers 422 without reaching the domain", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dusk", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dusk")
	})

	t.Run("creating past the hundred-theme ceiling is refused, while replacing one of the hundred still works", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		for i := 0; i < maxCustomThemes; i++ {
			id := "t" + string(rune('a'+i/26)) + string(rune('a'+i%26))
			apiPutTheme(t, h, owner, id, `{"id":"`+id+`","name":"`+id+`","colors":{"--color-bg":"#101418"}}`)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/onemore", owner,
			`{"id":"onemore","name":"One More","colors":{"--color-bg":"#101418"}}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"at most 100 custom themes may be saved — delete one first")
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "onemore")

		status, data = apiJSON(t, h, "PUT", "/api/themes/taa", owner,
			`{"id":"taa","name":"taa again","colors":{"--color-bg":"#000000"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "taa", "created": false, "order_idx": 0, "updated_at": apiAnyNumber,
		})
	})

	t.Run("a request carrying no credentials answers 401 and nothing is filed", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dusk", "",
			`{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dusk")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent, and nothing is filed", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/themes/dusk", housekeeper,
			`{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dusk")
	})
}

func TestHandleDeleteThemeApiThemesThemeIdDelete(t *testing.T) {
	t.Run("deleting a theme takes it off the list and the receipt says the active theme was left alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		apiPutTheme(t, h, owner, "dawn", `{"id":"dawn","name":"Dawn","colors":{"--color-bg":"#fffdf7"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/themes/dusk", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "dusk", "deleted": true, "display_theme_reset": false,
		})
		dashboard.wantFrames()
		apiThemeAbsent(t, h, owner, "dusk")
		apiThemeList(t, h, owner, map[string]any{"id": "dawn", "name": "Dawn"})
	})

	t.Run("deleting the ACTIVE theme clears display_theme in the same request and says so", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_theme":"dusk"}`); status != 200 {
			t.Fatalf("select theme: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/themes/dusk", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "dusk", "deleted": true, "display_theme_reset": true,
		})
		dashboard.wantFrames()
		if got := apiDisplayTheme(t, h, owner); got != "" {
			t.Fatalf("display_theme should have been cleared, got %q", got)
		}
	})

	t.Run("deleting a theme that is NOT the active one leaves display_theme pointing where it did", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		apiPutTheme(t, h, owner, "dawn", `{"id":"dawn","name":"Dawn","colors":{"--color-bg":"#fffdf7"}}`)
		if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_theme":"dusk"}`); status != 200 {
			t.Fatalf("select theme: %d %v", status, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/themes/dawn", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "dawn", "deleted": true, "display_theme_reset": false,
		})
		dashboard.wantFrames()
		if got := apiDisplayTheme(t, h, owner); got != "dusk" {
			t.Fatalf("display_theme should still be dusk, got %q", got)
		}
	})

	t.Run("an id nothing carries answers 404 naming it and deletes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/themes/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "theme 'nope' not found")
		dashboard.wantFrames()
		apiThemeList(t, h, owner, map[string]any{"id": "dusk", "name": "Dusk"})
	})

	t.Run("a request carrying no credentials answers 401 and the theme is still there", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/themes/dusk", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiThemeList(t, h, owner, map[string]any{"id": "dusk", "name": "Dusk"})
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent, and the theme is still there", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/themes/dusk", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiThemeList(t, h, owner, map[string]any{"id": "dusk", "name": "Dusk"})
	})

	t.Run("an ignored request body does not change theme deletion", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiPutTheme(t, h, owner, "dusk", `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"}}`)

		status, data := apiJSON(t, h, "DELETE", "/api/themes/dusk", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "dusk", "deleted": true, "display_theme_reset": false,
		})
		apiThemeAbsent(t, h, owner, "dusk")
	})
}

func TestDecodeStoredThemeBundle(t *testing.T) {
	t.Run("decodes the stored bundle and prunes wording codes this build does not know", func(t *testing.T) {
		row := CustomTheme{
			ID: "dusk",
			Bundle: `{"id":"dusk","name":"Dusk","colors":{"--color-bg":"#101418"},` +
				`"wording":{"zh":{"nav.tasks":"活兒","no.such.code":"gone"}}}`,
		}
		got, err := decodeStoredThemeBundle(row)
		if err != nil {
			t.Fatalf("decodeStoredThemeBundle: %v", err)
		}
		if got.Id != "dusk" || got.Name != "Dusk" || got.Colors["--color-bg"] != "#101418" {
			t.Fatalf("decoded bundle: %+v", got)
		}
		if got.Wording == nil || !reflect.DeepEqual(*got.Wording, map[string]map[string]string{
			"zh": {"nav.tasks": "活兒"},
		}) {
			t.Fatalf("wording prune: %+v", got.Wording)
		}
	})

	t.Run("returns the row id in an invalid-bundle error", func(t *testing.T) {
		_, err := decodeStoredThemeBundle(CustomTheme{ID: "dusk", Bundle: `{not json`})
		if err == nil {
			t.Fatal("decodeStoredThemeBundle: want an error")
		}
		if err.Error() != `stored theme "dusk" is not a decodable bundle: invalid character 'n' looking for beginning of object key string` {
			t.Fatalf("decodeStoredThemeBundle error: got %q", err)
		}
	})
}

func TestMarshalThemeBundle(t *testing.T) {
	got, err := marshalThemeBundle(ThemeBundleDTO{
		Id: "dusk", Name: "Dusk",
		Colors: map[string]string{"--color-text": "#ffffff", "--color-bg": "#101418"},
	})
	if err != nil {
		t.Fatalf("marshalThemeBundle: %v", err)
	}
	if got != `{"colors":{"--color-bg":"#101418","--color-text":"#ffffff"},"id":"dusk","name":"Dusk"}` {
		t.Fatalf("marshalThemeBundle: got %q", got)
	}
}

func TestDisplayThemeExists(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	if err := d.PutCustomTheme("dusk", `{"id":"dusk"}`); err != nil {
		t.Fatalf("PutCustomTheme: %v", err)
	}

	for _, tc := range []struct {
		theme string
		want  bool
	}{
		{theme: "", want: true},
		{theme: "office", want: true},
		{theme: "dusk", want: true},
		{theme: "missing", want: false},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			got, err := api.displayThemeExists(tc.theme)
			if err != nil {
				t.Fatalf("displayThemeExists(%q): %v", tc.theme, err)
			}
			if got != tc.want {
				t.Fatalf("displayThemeExists(%q): want %v, got %v", tc.theme, tc.want, got)
			}
		})
	}
}
