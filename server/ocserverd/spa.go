package main

// spa.go — the dashboard SPA embedded in the single binary. frontend/dist/ is a
// gitignored build artifact and go:embed cannot reach outside the module, so
// bin/build-webdist copies the build into webdist/; the committed .gitkeep keeps
// the embed pattern alive, so a build without the frontend still compiles and
// serves the build hint at "/".
//
// The fallback is deliberately OUT of the declarative route table (a static
// mount carries no auth label and derives no MCP tool). It holds only the bare
// "/" pattern, so every declared route wins.

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// `all:` tolerates the .gitkeep-only placeholder state.
//
//go:embed all:webdist
var webdistEmbed embed.FS

func webdistFS() fs.FS {
	sub, err := fs.Sub(webdistEmbed, "webdist")
	if err != nil {
		panic(err)
	}
	return sub
}

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

func newFallbackHandler(specs []RouteSpec, dist fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Route template hit ⇒ the method is wrong: a right-method request
		// would have matched the row's mux pattern and never reached here.
		for _, spec := range specs {
			if pathMatchesTemplate(spec.Path, path) {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
		}

		if strings.HasPrefix(path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		hasIndex := false
		if f, err := dist.Open("index.html"); err == nil {
			f.Close()
			hasIndex = true
		}

		name := strings.TrimPrefix(path, "/")
		if name != "" && !strings.HasSuffix(name, "/") {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				http.ServeFileFS(w, r, dist, name)
				return
			}
		}

		if last := path[strings.LastIndex(path, "/")+1:]; strings.Contains(last, ".") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

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
