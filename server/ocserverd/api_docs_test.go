// Skeleton generated from server/ocserverd/api_docs.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

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

func TestHandleListDocsApiDocsGet(t *testing.T) {
	t.Run("a well-formed GET /api/docs answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/docs request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/docs reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/docs request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleGetDocApiDocsSlugGet(t *testing.T) {
	t.Run("a well-formed GET /api/docs/{slug} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/docs/{slug} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/docs/{slug} reaches this handler with slug bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/docs/{slug} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleGetDocAssetApiDocsAssetsNameGet(t *testing.T) {
	t.Run("a well-formed GET /api/docs/assets/{name} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/docs/assets/{name} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/docs/assets/{name} reaches this handler with name bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/docs/assets/{name} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
