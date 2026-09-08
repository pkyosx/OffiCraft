// Skeleton generated from server/ocserverd/api_docs.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDocTitleUsesFirstHeadingOrSlug(t *testing.T) {
	tests := []struct {
		name string
		md   string
		slug string
		want string
	}{
		{name: "first heading", md: "intro\n# Product guide\n# Later", slug: "guide", want: "Product guide"},
		{name: "trimmed heading", md: "  #  Spaced title  \n", slug: "guide", want: "Spaced title"},
		{name: "no heading", md: "intro\n## Section", slug: "guide", want: "guide"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := docTitle(tt.md, tt.slug); got != tt.want {
				t.Fatalf("docTitle(%q, %q) = %q, want %q", tt.md, tt.slug, got, tt.want)
			}
		})
	}
}

func TestRewriteDocAssetPathsMakesRelativeImagesAbsolute(t *testing.T) {
	md := "![one](assets/one.png) ![two](./assets/two.svg) ![url](https://example.com/x.png)"
	want := "![one](/api/docs/assets/one.png) ![two](/api/docs/assets/two.svg) ![url](https://example.com/x.png)"
	if got := rewriteDocAssetPaths(md); got != want {
		t.Fatalf("rewriteDocAssetPaths() = %q, want %q", got, want)
	}
}

func TestDocOrderRankPlacesKnownSlugsBeforeUnknown(t *testing.T) {
	if got := docOrderRank("why"); got != 0 {
		t.Fatalf("docOrderRank(why) = %d, want 0", got)
	}
	if got := docOrderRank("troubleshooting"); got != len(docReadingOrder)-1 {
		t.Fatalf("docOrderRank(troubleshooting) = %d, want %d", got, len(docReadingOrder)-1)
	}
	if got := docOrderRank("new-doc"); got != len(docReadingOrder) {
		t.Fatalf("docOrderRank(new-doc) = %d, want %d", got, len(docReadingOrder))
	}
}

func TestListDocsFromReturnsTopLevelMarkdownInReadingOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"zebra.md":          {Data: []byte("no heading")},
		"alpha.md":          {Data: []byte("# Alpha")},
		"why.md":            {Data: []byte("# Why")},
		"install.md":        {Data: []byte("# Install")},
		"notes.txt":         {Data: []byte("ignored")},
		".gitkeep":          {Data: nil},
		"assets/image.png":  {Data: []byte("ignored asset")},
		"assets/nested.dat": {Data: []byte("ignored asset")},
	}

	got, err := listDocsFrom(fsys)
	if err != nil {
		t.Fatalf("listDocsFrom() error = %v", err)
	}
	want := []docSummaryDTO{
		{Slug: "why", Title: "Why"},
		{Slug: "install", Title: "Install"},
		{Slug: "alpha", Title: "Alpha"},
		{Slug: "zebra", Title: "zebra"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listDocsFrom() = %#v, want %#v", got, want)
	}
}

func TestReadDocFromReturnsRewrittenDocumentOrNil(t *testing.T) {
	fsys := fstest.MapFS{
		"guide.md": {Data: []byte("# Guide\n![diagram](./assets/diagram.png)")},
	}

	got := readDocFrom(fsys, "guide")
	if got == nil {
		t.Fatal("readDocFrom() returned nil for an existing document")
	}
	want := &docDTO{
		Slug:       "guide",
		Title:      "Guide",
		MarkdownMD: "# Guide\n![diagram](/api/docs/assets/diagram.png)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readDocFrom() = %#v, want %#v", got, want)
	}

	for _, slug := range []string{"missing", "", "../guide", "nested/guide"} {
		if got := readDocFrom(fsys, slug); got != nil {
			t.Fatalf("readDocFrom(%q) = %#v, want nil", slug, got)
		}
	}
}

func TestReadDocAssetFromReturnsBytesAndContentTypeOrFalse(t *testing.T) {
	fsys := fstest.MapFS{
		"assets/icon.png":   {Data: []byte("png bytes")},
		"assets/data.weird": {Data: []byte("unknown bytes")},
	}

	got, contentType, ok := readDocAssetFrom(fsys, "icon.png")
	if !ok || contentType != "image/png" || !bytes.Equal(got, []byte("png bytes")) {
		t.Fatalf("readDocAssetFrom(icon.png) = (%q, %q, %t), want (%q, image/png, true)", got, contentType, ok, "png bytes")
	}

	got, contentType, ok = readDocAssetFrom(fsys, "data.weird")
	if !ok || contentType != "application/octet-stream" || !bytes.Equal(got, []byte("unknown bytes")) {
		t.Fatalf("readDocAssetFrom(data.weird) = (%q, %q, %t), want (%q, application/octet-stream, true)", got, contentType, ok, "unknown bytes")
	}

	for _, name := range []string{"missing.png", "", "../icon.png", "nested/icon.png"} {
		got, contentType, ok := readDocAssetFrom(fsys, name)
		if ok || got != nil || contentType != "" {
			t.Fatalf("readDocAssetFrom(%q) = (%q, %q, %t), want (nil, empty, false)", name, got, contentType, ok)
		}
	}
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
