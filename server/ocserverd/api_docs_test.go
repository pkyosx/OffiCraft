// Skeleton generated from server/ocserverd/api_docs.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDocTitle(t *testing.T) {
	t.Skip("TODO: docTitle extracts a doc's display title: the first \"# \" heading, else the slug (a doc with no heading still addresses/lists honestly).")
}

func TestRewriteDocAssetPaths(t *testing.T) {
	t.Skip("TODO: rewriteDocAssetPaths makes a doc's RELATIVE image references resolvable from any render surface: `](assets/x.png)` / `](./assets/x.png)` → the absolute served asset endpoint.")
}

func TestDocOrderRank(t *testing.T) {
	t.Skip("TODO: docOrderRank returns a slug's position in docReadingOrder, or len(list) for an unranked slug so it falls to the tail (alphabetical among the unranked).")
}

func TestListDocsFrom(t *testing.T) {
	t.Skip("TODO: listDocsFrom reads every top-level *.md in the doc FS (the assets/ subtree and .gitkeep are skipped), sorted by the guide's reading order (docReadingOrder; unranked docs fall to the tail, alphabetical among themselves) for a coherent, stable surface.")
}

func TestReadDocFrom(t *testing.T) {
	t.Skip("TODO: readDocFrom folds one doc by slug (nil = unknown → caller 404s).")
}

func TestReadDocAssetFrom(t *testing.T) {
	t.Skip("TODO: readDocAssetFrom returns a doc image's bytes + its content-type (ok=false = a missing/traversing name → the caller 404s).")
}

// apiDocList drives GET /api/docs and asserts the WHOLE array — the route
// answers a bare JSON array, which apiJSON's object decode cannot carry.
func apiDocList(t *testing.T, h http.Handler, credential string, want ...map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/docs", credential, "")
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

// apiGuideIndex is the guide as this build carries it: every top-level doc in
// the embed, titled by its first heading, in the reading order the endpoint
// promises.
func apiGuideIndex() []map[string]any {
	return []map[string]any{
		{"slug": "why", "title": "為什麼是 OffiCraft"},
		{"slug": "install", "title": "安裝、升級與移除"},
		{"slug": "quickstart", "title": "你的第一個辦公室"},
		{"slug": "interface", "title": "介面說明"},
		{"slug": "members", "title": "成員與外包"},
		{"slug": "tasks", "title": "任務是怎麼運作的"},
		{"slug": "settings", "title": "設定與參數"},
		{"slug": "theme", "title": "主題（外觀與用語）"},
		{"slug": "best-practices", "title": "建議用法"},
		{"slug": "architecture", "title": "架構與運作原理"},
		{"slug": "glossary", "title": "名詞表"},
		{"slug": "mobile", "title": "在手機上用控制台"},
		{"slug": "troubleshooting", "title": "常見問題與排解"},
	}
}

func TestHandleListDocsApiDocsGet(t *testing.T) {
	t.Run("every guide doc lists exactly once, titled by its first heading, in the guide's reading order rather than alphabetically", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiDocList(t, h, owner, apiGuideIndex()...)
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity is served the same list, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		apiDocList(t, h, housekeeper, apiGuideIndex()...)
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("a GET /api/docs request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) {
		t.Skip("structurally unproducible: this GET decodes no request body and the " +
			"stack carries no content-type or size middleware, so no wire-layer 4xx " +
			"exists to observe — measured: a `{{{` body on this route still answers 200.")
	})
}

func TestHandleGetDocApiDocsSlugGet(t *testing.T) {
	t.Run("one doc comes back as its slug, its first heading as the title, and its markdown — and nothing else", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/why", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"slug": "why", "title": "為什麼是 OffiCraft", "markdown_md": apiAnyString,
		})
		body, _ := data["markdown_md"].(string)
		if !strings.HasPrefix(body, "# 為什麼是 OffiCraft\n") {
			t.Fatalf("the doc body does not open with its own heading: %.60q", body)
		}
		dashboard.wantFrames()
	})

	t.Run("the slug in the path picks the doc, and the neighbouring doc is not what comes back", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/docs/glossary", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"slug": "glossary", "title": "名詞表", "markdown_md": apiAnyString,
		})
	})

	t.Run("a doc's relative image references come back rewritten to the served asset endpoint, so the same bytes render anywhere", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/docs/tasks", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		body, _ := data["markdown_md"].(string)
		if !strings.Contains(body, "](/api/docs/assets/cockpit-task.png)") {
			t.Fatalf("the doc's image reference was not rewritten to the asset endpoint")
		}
		if strings.Contains(body, "](assets/") || strings.Contains(body, "](./assets/") {
			t.Fatalf("a relative image reference survived into the served markdown")
		}
	})

	t.Run("a slug nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "doc 'nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity reads it too, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/why", housekeeper, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"slug": "why", "title": "為什麼是 OffiCraft", "markdown_md": apiAnyString,
		})
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/why", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("a GET /api/docs/{slug} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) {
		t.Skip("structurally unproducible: this GET decodes no request body and the " +
			"stack carries no content-type or size middleware, so no wire-layer 4xx " +
			"exists to observe — measured: a `{{{` body on this route still answers 200.")
	})
}

func TestHandleGetDocAssetApiDocsAssetsNameGet(t *testing.T) {
	t.Run("a referenced screenshot is served as its own bytes under its own content type, not as JSON", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/docs/assets/cockpit-task.png", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Fatalf("content type was %q", got)
		}
		if !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("the answer does not open with the PNG magic bytes")
		}
		dashboard.wantFrames()
	})

	t.Run("the name in the path picks the file, and its own extension decides the content type", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		rec := apiRequest(t, h, "GET", "/api/docs/assets/architecture-overview.svg", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
			t.Fatalf("content type was %q", got)
		}
		if !bytes.HasPrefix(rec.Body.Bytes(), []byte("<svg")) {
			t.Fatalf("the answer is not the vector diagram: %.40q", rec.Body.String())
		}
	})

	t.Run("a name that tries to climb out of the assets directory answers 404 rather than serving a doc", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/assets/..%2Fwhy.md", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "asset not found")
		dashboard.wantFrames()
	})

	t.Run("a name no asset carries answers 404 without naming what was asked for", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/assets/nope.png", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "asset not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity reads it too, because this row sits at the machine floor", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		rec := apiRequest(t, h, "GET", "/api/docs/assets/cockpit-task.png", housekeeper, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Fatalf("content type was %q", got)
		}
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/docs/assets/cockpit-task.png", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("a GET /api/docs/assets/{name} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) {
		t.Skip("structurally unproducible: this GET decodes no request body and the " +
			"stack carries no content-type or size middleware, so no wire-layer 4xx " +
			"exists to observe — measured: a `{{{` body on this route still answers 200.")
	})
}
