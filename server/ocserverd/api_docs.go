package main

// Product-guide surface: docs/guide/ is baked into the binary (docsdist embed,
// assets.go). The list + content routes also derive the list_docs / get_doc MCP
// tools an assistant agent calls; the asset route is MCP-excluded. Reads are
// embed-only, never a docs/ under the CWD.

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
)

const docAssetURLPrefix = "/api/docs/assets/"

// build-docsdist FLATTENS docs/guide/ into one directory, so the basename is
// all a slug can carry — a client turning a doc-relative link into a slug must
// apply exactly this rule to the basename.
func docSlug(filename string) string {
	return strings.TrimSuffix(filename, ".md")
}

func docTitle(md, slug string) string {
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return slug
}

func rewriteDocAssetPaths(md string) string {
	md = strings.ReplaceAll(md, "](./assets/", "]("+docAssetURLPrefix)
	md = strings.ReplaceAll(md, "](assets/", "]("+docAssetURLPrefix)
	return md
}

var docReadingOrder = []string{
	"why",
	"install",
	"quickstart",
	"interface",
	"members",
	"tasks",
	"settings",
	"theme",
	"best-practices",
	"architecture",
	"glossary",
	"mobile",
	"troubleshooting",
}

func docOrderRank(slug string) int {
	for i, s := range docReadingOrder {
		if s == slug {
			return i
		}
	}
	return len(docReadingOrder)
}

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
	sort.Slice(out, func(i, j int) bool {
		ri, rj := docOrderRank(out[i].Slug), docOrderRank(out[j].Slug)
		if ri != rj {
			return ri < rj
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

func readDocFrom(fsys fs.FS, slug string) *docDTO {
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		return nil
	}
	raw, err := fs.ReadFile(fsys, slug+".md")
	if err != nil {
		return nil
	}
	md := string(raw)
	return &docDTO{
		Slug:       slug,
		Title:      docTitle(md, slug),
		MarkdownMD: rewriteDocAssetPaths(md),
	}
}

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

func (s *apiServer) HandleListDocsApiDocsGet(w http.ResponseWriter, r *http.Request) {
	docs, err := listDocsFrom(docsdistFS())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *apiServer) HandleGetDocApiDocsSlugGet(w http.ResponseWriter, r *http.Request, slug string) {
	doc := readDocFrom(docsdistFS(), slug)
	if doc == nil {
		writeError(w, http.StatusNotFound, "doc '"+slug+"' not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

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
