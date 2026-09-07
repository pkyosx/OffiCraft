// Skeleton generated from server/ocserverd/api_doc_sizes.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHandlePeekDocSizesApiDocSizesGet(t *testing.T) {
	t.Run("a well-formed GET /api/doc-sizes answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/doc-sizes request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/doc-sizes reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/doc-sizes request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
