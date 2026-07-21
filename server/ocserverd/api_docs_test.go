package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func docFS() fstest.MapFS {
	return fstest.MapFS{
		"guide.md":       {Data: []byte("# Guide\n\nField glossary lives here.\n\n![map](assets/map.png)\n")},
		"tasks.md":       {Data: []byte("# Tasks\n\nSee ![flow](./assets/flow.png).\n")},
		"assets/map.png": {Data: []byte("\x89PNGmapbytes")},
		".gitkeep":       {Data: []byte("")},
	}
}

func TestListDocsFrom(t *testing.T) {
	got, err := listDocsFrom(docFS())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// *.md only, assets/ + .gitkeep skipped, sorted by slug.
	if len(got) != 2 {
		t.Fatalf("want 2 docs, got %d: %v", len(got), got)
	}
	if got[0].Slug != "guide" || got[0].Title != "Guide" {
		t.Errorf("first row wrong: %+v", got[0])
	}
	if got[1].Slug != "tasks" || got[1].Title != "Tasks" {
		t.Errorf("second row wrong: %+v", got[1])
	}
}

func TestReadDocFromRewritesRelativeImagePaths(t *testing.T) {
	doc := readDocFrom(docFS(), "guide")
	if doc == nil {
		t.Fatal("known slug must fold")
	}
	if doc.Title != "Guide" {
		t.Errorf("title: %q", doc.Title)
	}
	if want := "![map](/api/docs/assets/map.png)"; !strings.Contains(doc.MarkdownMD, want) {
		t.Errorf("relative image path not rewritten:\n%s", doc.MarkdownMD)
	}
	// The ./assets/ form rewrites too (generic for O-46 docs).
	if want := "![flow](/api/docs/assets/flow.png)"; !strings.Contains(readDocFrom(docFS(), "tasks").MarkdownMD, want) {
		t.Errorf("./assets/ form not rewritten")
	}
}

func TestReadDocFromUnknownSlugIsNil(t *testing.T) {
	if readDocFrom(docFS(), "does-not-exist") != nil {
		t.Error("unknown slug must be nil (→ 404)")
	}
	// A traversing slug must never escape the doc root.
	if readDocFrom(docFS(), "../secret") != nil {
		t.Error("traversing slug must be nil")
	}
}

func TestReadDocAssetFromServesBytesWithContentType(t *testing.T) {
	raw, ct, ok := readDocAssetFrom(docFS(), "map.png")
	if !ok {
		t.Fatal("embedded asset must serve")
	}
	if string(raw) != "\x89PNGmapbytes" {
		t.Errorf("asset bytes: %q", raw)
	}
	if ct != "image/png" {
		t.Errorf("content-type: %q", ct)
	}
	if _, _, ok := readDocAssetFrom(docFS(), "missing.png"); ok {
		t.Error("missing asset must 404")
	}
	if _, _, ok := readDocAssetFrom(docFS(), "../map.png"); ok {
		t.Error("traversing asset name must 404")
	}
}

// TestDocsMcpToolsCallableByAssistantAgent is the load-bearing proof for this
// ticket: Mira (an assistant member → admin_agent principal) can actually call
// the new read-only MCP tools through the SAME wired stack (auth gate + RBAC
// choke + param binding) a live agent uses. The docs read tools sit at the
// machine floor, so an admin_agent (rank 2 ≥ 0) passes; the assistant is the
// intended caller of get_doc.
func TestDocsMcpToolsCallableByAssistantAgent(t *testing.T) {
	api := newTasksTestServer(t)
	// Seed an assistant member — role_key "assistant" classifies as admin_agent
	// (authz.classifyMember), exactly the Mira principal.
	mira := fullMember("mira")
	if err := api.dal.PutMember(mira); err != nil {
		t.Fatalf("seed assistant: %v", err)
	}

	secret := []byte("tasks-test-secret")
	h, err := buildHandler(specsFor(api), secret, api.dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	now := time.Now().Unix()
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	// list_docs — the assistant's pre-read index. The README is always staged.
	res := docToolResult(t, srv.URL, miraTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_docs","arguments":{}}}`)
	if res["isError"] != false {
		t.Fatalf("assistant list_docs must succeed: %v", res)
	}
	listText := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(listText, `"readme"`) {
		t.Fatalf("list_docs must carry the staged docs: %s", listText)
	}

	// get_doc — the field/feature answer source. Read one staged doc in full.
	res = docToolResult(t, srv.URL, miraTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_doc","arguments":{"slug":"readme"}}}`)
	if res["isError"] != false {
		t.Fatalf("assistant get_doc must succeed: %v", res)
	}
	sc := res["structuredContent"].(map[string]any)
	if sc["slug"] != "readme" || len(sc["markdown_md"].(string)) == 0 {
		t.Fatalf("get_doc payload wrong: %v", sc)
	}

	// Unknown slug forwards the REST 404 as an isError result (never fabricated).
	res = docToolResult(t, srv.URL, miraTok,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_doc","arguments":{"slug":"nope"}}}`)
	if res["isError"] != true ||
		res["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("unknown slug must surface the REST 404 envelope: %v", res)
	}
}

func docToolResult(t *testing.T, url, token, body string) map[string]any {
	t.Helper()
	payload := postMCP(t, url, token, body)
	if err, present := payload["error"]; present {
		t.Fatalf("expected a result envelope, got error: %v", err)
	}
	return payload["result"].(map[string]any)
}

func TestGetDocHandlerServesStagedEmbed(t *testing.T) {
	// Through the real embed (docsdist staged by CI): the README lists + reads.
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleListDocsApiDocsGet(rec, httptest.NewRequest("GET", "/api/docs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status: %d", rec.Code)
	}
	var list []docSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list unmarshal: %v (%s)", err, rec.Body)
	}
	found := false
	for _, d := range list {
		if d.Slug == "readme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("staged embed must list the README (slug readme): %v", list)
	}

	rec = httptest.NewRecorder()
	api.HandleGetDocApiDocsSlugGet(rec, httptest.NewRequest("GET", "/api/docs/readme", nil), "readme")
	if rec.Code != http.StatusOK || len(rec.Body.String()) == 0 {
		t.Fatalf("get staged doc: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.HandleGetDocApiDocsSlugGet(rec, httptest.NewRequest("GET", "/api/docs/nope", nil), "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slug must 404, got %d", rec.Code)
	}
}

// TestDocsdistEmbedsExactlyReadmeAndGuide is the embed-scope guard: the staged
// doc SET must be exactly {readme} ∪ docs/guide/*.md — no more, no less. Its job
// is to pin the SCOPE of what build-docsdist collects: the README plus the whole
// docs/guide/ folder, and nothing from OUTSIDE that scope (docs/dev.md,
// docs/design/, or any other repo doc must never leak into the product embed).
//
// It deliberately does NOT gate the CONTENTS of docs/guide/: a draft dropped in
// there IS collected (it lands in both `want` and `got`, since both derive from
// the same folder), and that is by design — "anything under docs/guide/ is
// product content meant to ship" is the owner's folder convention, and whether
// a given file belongs there is a pre-land human review call, not this test's
// job. Content-level gating (frontmatter draft flags, allowlists) was
// explicitly rejected: it reintroduces the hand-maintained list the folder
// convention exists to avoid. Verifies against the SOURCE dirs on disk (repo
// root ../..), so it fails when build-docsdist's collection drifts OUT of scope.
func TestDocsdistEmbedsExactlyReadmeAndGuide(t *testing.T) {
	repoRoot := "../.."
	want := map[string]bool{"readme": true}
	guideEntries, err := os.ReadDir(filepath.Join(repoRoot, "docs", "guide"))
	if err != nil {
		t.Fatalf("read docs/guide: %v", err)
	}
	for _, e := range guideEntries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			want[docSlug(e.Name())] = true
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "README.md")); err != nil {
		t.Fatalf("repo README.md must exist (the guide front page): %v", err)
	}

	docs, err := listDocsFrom(docsdistFS())
	if err != nil {
		t.Fatalf("list staged: %v", err)
	}
	got := map[string]bool{}
	for _, d := range docs {
		got[d.Slug] = true
	}

	for slug := range want {
		if !got[slug] {
			t.Errorf("expected doc %q missing from the embed", slug)
		}
	}
	for slug := range got {
		if !want[slug] {
			t.Errorf("OUT-OF-SCOPE doc %q in the embed — only {readme} ∪ docs/guide/*.md may ship "+
				"(did docs/dev|design or another repo doc leak into build-docsdist?)", slug)
		}
	}
	// Explicit sentinel: the developer docs must never ride along.
	if got["dev"] {
		t.Error("docs/dev.md leaked into the product embed")
	}
}
