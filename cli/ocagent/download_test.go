// Skeleton generated from cli/ocagent/download.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestNewStreamingClient(t *testing.T) {
	t.Skip("TODO: newStreamingClient builds the HTTP client both blob directions share (download's fetch, upload's send).")
}

func TestCmdDownload(t *testing.T) {
	t.Skip("TODO: cmdDownload implements `ocagent download`.")
}

func TestFilenameFromDisposition(t *testing.T) {
	t.Skip("TODO: filenameFromDisposition extracts the served filename from a Content-Disposition header, PREFERRING the RFC 5987 `filename*=UTF-8”<pct-encoded>` parameter (the true, possibly non-ASCII name the server always sends alongside the ASCII fallback) over the plain `filename=\"…\"`.")
}

func TestSanitizeFilename(t *testing.T) {
	t.Skip("TODO: sanitizeFilename reduces a server-supplied (i.e.")
}
