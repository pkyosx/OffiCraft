package main

// spa.go — the embedded dashboard SPA base (the Go twin of
// the retired Python service/static.py, upgraded to the single-binary target: Seth's call
// that frontend + backend ship as ONE binary, so the built SPA rides inside
// ocserverd via go:embed instead of being read off disk).
//
// Staging: frontend/dist/ is a gitignored BUILD ARTIFACT (frontend/.gitignore),
// and go:embed cannot reach outside the module directory — so bin/build-webdist
// copies the fresh `npm run build` output into webdist/ here before a
// SPA-carrying binary is built. webdist/ itself stays gitignored except the
// committed .gitkeep, which keeps the directory (and therefore the //go:embed
// pattern) alive on a clean checkout: a build WITHOUT the frontend staged
// compiles fine and serves the friendly build hint at "/" — exactly the
// Python mount_spa degradation, no build tags, no fake dist.
//
// Like the Python side, this is an INFRASTRUCTURE fallback, deliberately kept
// OUT of the declarative route table: a static-asset mount carries no auth
// label and derives no MCP tool. Precedence is structural — every RouteSpec
// row registers method+path on the mux first, and the fallback holds only the
// bare "/" pattern, so declared routes always win. The fallback then answers,
// in order:
//
//   1. a path matching a route TEMPLATE (wrong method — the mux would have
//      routed a right-method hit) → 405 through the unified error envelope;
//   2. any other /api/* path → 404 envelope (an API typo is an honest API
//      error, never the HTML shell);
//   3. an embedded static file → served as itself;
//   4. an asset-like miss (the last segment carries an extension) → 404
//      envelope (a missing file is never rewritten to the shell);
//   5. anything else → index.html (the SPA catch-all for client-side routes),
//      or the build hint when no SPA is staged.

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// The staged SPA build (see the module comment). `all:` keeps hashed dotfile-
// free asset trees intact and tolerates the .gitkeep-only placeholder state.
//
//go:embed all:webdist
var webdistEmbed embed.FS

// webdistFS returns the embedded SPA root (the webdist/ subtree).
func webdistFS() fs.FS {
	sub, err := fs.Sub(webdistEmbed, "webdist")
	if err != nil {
		// The embed directive guarantees the subtree exists; reaching this is a
		// programmer error, and failing loud beats serving a broken shell.
		panic(err)
	}
	return sub
}

// missingDistHTML is shown at "/" when no SPA build is staged — the byte-for-
// byte twin of service.static._MISSING_DIST_HTML (same tokens, same fix).
const missingDistHTML = "<!doctype html><html lang='en'><head><meta charset='utf-8'>" +
	"<meta name='viewport' content='width=device-width,initial-scale=1'>" +
	"<title>officraft</title></head>" +
	`<body style="font-family:system-ui,sans-serif;background:#191C24;` +
	`color:#E7E8EE;padding:3rem;line-height:1.6">` +
	"<h1>officraft</h1>" +
	"<p>The API is running, but the dashboard has not been built yet.</p>" +
	`<pre style="background:#242832;padding:1rem;border-radius:8px;` +
	`color:#6FD6B0">cd frontend &amp;&amp; npm install &amp;&amp; npm run build</pre>` +
	"<p>API is live: " +
	"<a style='color:#6FD6B0' href='/api/health'>/api/health</a></p>" +
	"</body></html>"

// pathMatchesTemplate reports whether path matches a route path template
// ("/api/members/{member_id}"-style; a {param} segment matches any single
// non-empty segment).
func pathMatchesTemplate(template, path string) bool {
	ts := strings.Split(template, "/")
	ps := strings.Split(path, "/")
	if len(ts) != len(ps) {
		return false
	}
	for i := range ts {
		if strings.HasPrefix(ts[i], "{") && strings.HasSuffix(ts[i], "}") {
			if ps[i] == "" {
				return false
			}
			continue
		}
		if ts[i] != ps[i] {
			return false
		}
	}
	return true
}

// newFallbackHandler builds the "/" fallback over the route table + an SPA
// filesystem (the embedded webdist in production; tests inject fstest.MapFS).
// See the module comment for the decision ladder.
func newFallbackHandler(specs []RouteSpec, dist fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 1. Route template hit ⇒ the method is wrong (a right-method request
		// would have matched the row's mux pattern, never reached here).
		for _, spec := range specs {
			if pathMatchesTemplate(spec.Path, path) {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
		}

		// 2. Unknown API path: an honest API 404, never the HTML shell.
		if strings.HasPrefix(path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		hasIndex := false
		if f, err := dist.Open("index.html"); err == nil {
			f.Close()
			hasIndex = true
		}

		// 3. An embedded static file serves as itself.
		name := strings.TrimPrefix(path, "/")
		if name != "" && !strings.HasSuffix(name, "/") {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				http.ServeFileFS(w, r, dist, name)
				return
			}
		}

		// 4. An asset-like miss (extension in the last segment) stays a 404 —
		// a missing file is never rewritten to the shell (static.py contract).
		if last := path[strings.LastIndex(path, "/")+1:]; strings.Contains(last, ".") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		// 5. The SPA catch-all: client-side routes get index.html; with no SPA
		// staged, "/" answers the friendly build hint and the rest honest 404s.
		if hasIndex {
			http.ServeFileFS(w, r, dist, "index.html")
			return
		}
		if path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(missingDistHTML))
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	})
}
