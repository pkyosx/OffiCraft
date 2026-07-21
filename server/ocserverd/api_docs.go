package main

// api_docs.go — the product-guide surface: the repo-root docs/guide/ tree baked
// into the binary (docsdist embed, assets.go) served THREE ways off ONE source:
// GET /api/docs (list), GET /api/docs/{slug} (one doc in full), and
// GET /api/docs/assets/{name} (the referenced images). The list + content reads
// are declarative gated routes at the machine floor, so they also derive the
// list_docs / get_doc MCP tools an assistant agent (Mira) calls to answer
// "what is this field / where do I set X"; the asset route is MCP-excluded (a
// binary is not a callable tool). Reads are EMBED-ONLY — the doc bytes this
// binary was built with, never a docs/ under the CWD. The *From cores take an
// injectable fs.FS (tests pass fstest.MapFS; production passes docsdistFS()).

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
)

// docAssetURLPrefix is the served path a doc's relative image reference is
// rewritten to point at (GET /api/docs/assets/{name}). Kept in one place so the
// rewrite and the asset route agree.
const docAssetURLPrefix = "/api/docs/assets/"

// docSlug maps a docsdist filename ("why.md") to its addressable slug ("why";
// the repo README is staged as readme.md → "readme").
func docSlug(filename string) string {
	return strings.TrimSuffix(filename, ".md")
}

// docTitle extracts a doc's display title: the first "# " heading, else the
// slug (a doc with no heading still addresses/lists honestly).
func docTitle(md, slug string) string {
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return slug
}

// rewriteDocAssetPaths makes a doc's RELATIVE image references resolvable from
// any render surface: `](assets/x.png)` / `](./assets/x.png)` → the absolute
// served asset endpoint. Generic on purpose — e.g. tasks.md references
// `assets/cockpit-task.png` and it resolves with no further change.
func rewriteDocAssetPaths(md string) string {
	md = strings.ReplaceAll(md, "](./assets/", "]("+docAssetURLPrefix)
	md = strings.ReplaceAll(md, "](assets/", "]("+docAssetURLPrefix)
	return md
}

// listDocsFrom reads every top-level *.md in the doc FS (the assets/ subtree and
// .gitkeep are skipped), sorted by slug for a stable surface.
func listDocsFrom(fsys fs.FS) ([]docSummaryDTO, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	out := []docSummaryDTO{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		slug := docSlug(e.Name())
		out = append(out, docSummaryDTO{Slug: slug, Title: docTitle(string(raw), slug)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// readDocFrom folds one doc by slug (nil = unknown → caller 404s).
func readDocFrom(fsys fs.FS, slug string) *docDTO {
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		return nil
	}
	raw, err := fs.ReadFile(fsys, slug+".md")
	if err != nil {
		return nil // missing file is an unknown slug, not a server error
	}
	md := string(raw)
	return &docDTO{
		Slug:       slug,
		Title:      docTitle(md, slug),
		MarkdownMD: rewriteDocAssetPaths(md),
	}
}

// readDocAssetFrom returns a doc image's bytes + its content-type (ok=false = a
// missing/traversing name → the caller 404s).
func readDocAssetFrom(fsys fs.FS, name string) ([]byte, string, bool) {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return nil, "", false
	}
	raw, err := fs.ReadFile(fsys, path.Join("assets", name))
	if err != nil {
		return nil, "", false
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return raw, ct, true
}

// GET /api/docs — list the product-guide docs (slug + title).
func (s *apiServer) HandleListDocsApiDocsGet(w http.ResponseWriter, r *http.Request) {
	docs, err := listDocsFrom(docsdistFS())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

// GET /api/docs/{slug} — one product-guide doc in full (unknown → 404).
func (s *apiServer) HandleGetDocApiDocsSlugGet(w http.ResponseWriter, r *http.Request, slug string) {
	doc := readDocFrom(docsdistFS(), slug)
	if doc == nil {
		writeError(w, http.StatusNotFound, "doc '"+slug+"' not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// GET /api/docs/assets/{name} — a doc's embedded image (bytes, not a tool).
// Unknown name → 404 (never the SPA shell, never a directory listing).
func (s *apiServer) HandleGetDocAssetApiDocsAssetsNameGet(w http.ResponseWriter, r *http.Request, name string) {
	raw, ct, ok := readDocAssetFrom(docsdistFS(), name)
	if !ok {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
