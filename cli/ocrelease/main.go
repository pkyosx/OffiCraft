// Command ocrelease is the PUBLISHER's client for the officraft release
// server (server/ocupdaterd) — the official release flow's two verbs:
//
//	ocrelease publish --file <binary> [--notes ...] [--git-sha ...] [--version ...]
//	    upload one built artifact → it lands in the BETA channel; the server
//	    generates the date-form version (vYYMMDD-NNNN) unless one is supplied.
//	ocrelease promote <version>
//	    stamp a published version GA (idempotent) — the GA channel's latest
//	    becomes this version.
//
// Credentials ride the ENVIRONMENT, never argv (argv is world-readable via
// ps — same posture as ocserverd's $OC_NEW_PASSWORD):
//
//	OC_UPDATER_URL    — the updater server base URL (e.g. http://127.0.0.1:8790)
//	OC_PUBLISH_TOKEN  — a publish token minted on the updater host
//	                    (ocupdaterd mint-publish-token)
//
// Naming (root CLAUDE.md §10): folder cli/ocrelease/ = module ocrelease =
// binary ocrelease; committed prebuilt lives at bin/ocrelease (CI step 1
// parity gate). Like the other cli/ modules this is stdlib-only; the network
// seam for tests is the base URL itself (httptest.Server).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	envUpdaterURL   = "OC_UPDATER_URL"
	envPublishToken = "OC_PUBLISH_TOKEN"
	// uploadTimeout bounds one publish upload end to end (the artifact runs
	// ~25MB; generous even over a slow tunnel). promote is a tiny JSON POST
	// and shares it.
	uploadTimeout = 5 * time.Minute
)

func usage(out io.Writer) {
	fmt.Fprintln(out, "usage: ocrelease <subcommand> [flags]")
	fmt.Fprintln(out, "  officraft release publisher (client of ocupdaterd).")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "subcommands:")
	fmt.Fprintln(out, "  publish --file <binary> [--notes <text>] [--git-sha <sha>] [--version <vYYMMDD-NNNN>]")
	fmt.Fprintln(out, "          upload one built artifact into the BETA channel (the server generates")
	fmt.Fprintln(out, "          the version unless --version is given) and print the released version")
	fmt.Fprintln(out, "  promote <version>")
	fmt.Fprintln(out, "          stamp a published version GA (idempotent) — GA clients see it as latest")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "credentials (environment, never argv):")
	fmt.Fprintln(out, "  "+envUpdaterURL+"    updater server base URL, e.g. http://127.0.0.1:8790")
	fmt.Fprintln(out, "  "+envPublishToken+"  publish token (mint on the updater host: ocupdaterd mint-publish-token)")
}

// realMain is the testable entrypoint: argv WITHOUT the program name, an env
// accessor, and the output sink (mirrors server/ocupdaterd/main.go realMain).
func realMain(argv []string, env func(string) string, out io.Writer) int {
	if len(argv) == 0 {
		usage(out)
		return 2
	}
	switch argv[0] {
	case "-h", "--help", "help":
		usage(out)
		return 0
	case "publish":
		return cmdPublish(argv[1:], env, out)
	case "promote":
		return cmdPromote(argv[1:], env, out)
	default:
		fmt.Fprintf(out, "[ocrelease] unknown subcommand %q\n\n", argv[0])
		usage(out)
		return 2
	}
}

// creds resolves the env credential pair, answering the actionable usage
// error itself (rc 2) when either is missing.
func creds(env func(string) string, out io.Writer) (baseURL, token string, ok bool) {
	baseURL = strings.TrimRight(strings.TrimSpace(env(envUpdaterURL)), "/")
	token = strings.TrimSpace(env(envPublishToken))
	if baseURL == "" || token == "" {
		fmt.Fprintf(out, "[ocrelease] %s and %s must both be set (env, not argv — the token leaks via ps)\n",
			envUpdaterURL, envPublishToken)
		return "", "", false
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		fmt.Fprintf(out, "[ocrelease] %s must be an absolute http(s) URL, got %q\n", envUpdaterURL, baseURL)
		return "", "", false
	}
	return baseURL, token, true
}

// releaseFace is the slice of ocupdaterd's release DTO this CLI reports on.
// ReleaseTag is the r-N serial (T-e9d1); "" when talking to a pre-serial
// updater, in which case the report falls back to the version string alone.
type releaseFace struct {
	Version    string   `json:"version"`
	ReleaseTag string   `json:"release_tag"`
	GitSHA     string   `json:"git_sha"`
	SHA256     string   `json:"sha256"`
	Size       int64    `json:"size"`
	ChannelGA  bool     `json:"channel_ga"`
	GAAt       *float64 `json:"ga_at"`
}

// releaseLabel names a release for humans: "r-N (version)" when the updater
// speaks serials, else the bare version string (pre-serial updater).
func (r releaseFace) releaseLabel() string {
	if r.ReleaseTag != "" {
		return fmt.Sprintf("%s (%s)", r.ReleaseTag, r.Version)
	}
	return r.Version
}

// errorEnvelope is the repo-wide {"error":{"code","message"}} shape.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// serverMessage extracts the server's own honest message from an error body,
// falling back to the raw body.
func serverMessage(body []byte) string {
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	return strings.TrimSpace(string(body))
}

// cmdPublish computes the artifact's sha256 itself (the server recomputes and
// refuses a mismatch — the double-digest is the corruption gate), streams the
// multipart upload, and prints the released version from the 201 body.
func cmdPublish(args []string, env func(string) string, out io.Writer) int {
	var file, notes, gitSHA, version string
	for i := 0; i < len(args); i++ {
		flagArg := func() (string, bool) {
			if i+1 >= len(args) {
				fmt.Fprintf(out, "[ocrelease] %s needs a value\n", args[i])
				return "", false
			}
			i++
			return args[i], true
		}
		var ok bool
		switch args[i] {
		case "--file", "-file":
			file, ok = flagArg()
		case "--notes", "-notes":
			notes, ok = flagArg()
		case "--git-sha", "-git-sha":
			gitSHA, ok = flagArg()
		case "--version", "-version":
			version, ok = flagArg()
		default:
			fmt.Fprintf(out, "[ocrelease] unknown flag %q\n\n", args[i])
			usage(out)
			return 2
		}
		if !ok {
			return 2
		}
	}
	if file == "" {
		fmt.Fprintln(out, "[ocrelease] publish needs --file <binary> (the built artifact to upload)")
		return 2
	}
	baseURL, token, ok := creds(env, out)
	if !ok {
		return 2
	}

	payload, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(out, "[ocrelease] cannot read %s: %v\n", file, err)
		return 1
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	fields := map[string]string{"sha256": digest, "notes": notes, "git_sha": gitSHA}
	if version != "" {
		fields["version"] = version
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			fmt.Fprintf(out, "[ocrelease] build upload: %v\n", err)
			return 1
		}
	}
	fw, err := mw.CreateFormFile("binary", filepath.Base(file))
	if err != nil {
		fmt.Fprintf(out, "[ocrelease] build upload: %v\n", err)
		return 1
	}
	if _, err := fw.Write(payload); err != nil {
		fmt.Fprintf(out, "[ocrelease] build upload: %v\n", err)
		return 1
	}
	if err := mw.Close(); err != nil {
		fmt.Fprintf(out, "[ocrelease] build upload: %v\n", err)
		return 1
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/publish", strings.NewReader(buf.String()))
	if err != nil {
		fmt.Fprintf(out, "[ocrelease] build request: %v\n", err)
		return 1
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	status, body, err := doRequest(req)
	if err != nil {
		fmt.Fprintf(out, "[ocrelease] cannot reach the updater server at %s: %v\n", baseURL, err)
		return 1
	}
	if status != http.StatusCreated {
		fmt.Fprintf(out, "[ocrelease] publish refused (%d): %s\n", status, serverMessage(body))
		return 1
	}
	var rel releaseFace
	if err := json.Unmarshal(body, &rel); err != nil {
		fmt.Fprintf(out, "[ocrelease] cannot decode the publish response: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "published %s (beta channel) — sha256 %s, %d bytes\n", rel.releaseLabel(), rel.SHA256, rel.Size)
	fmt.Fprintf(out, "verify on a beta client, then release it: ocrelease promote %s\n", rel.Version)
	return 0
}

// cmdPromote stamps one version GA over POST /api/promote.
func cmdPromote(args []string, env func(string) string, out io.Writer) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(out, "[ocrelease] usage: promote <version>   (e.g. promote v260714-0001)")
		return 2
	}
	version := args[0]
	baseURL, token, ok := creds(env, out)
	if !ok {
		return 2
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/promote",
		strings.NewReader(fmt.Sprintf(`{"version":%q}`, version)))
	if err != nil {
		fmt.Fprintf(out, "[ocrelease] build request: %v\n", err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	status, body, err := doRequest(req)
	if err != nil {
		fmt.Fprintf(out, "[ocrelease] cannot reach the updater server at %s: %v\n", baseURL, err)
		return 1
	}
	if status != http.StatusOK {
		fmt.Fprintf(out, "[ocrelease] promote refused (%d): %s\n", status, serverMessage(body))
		return 1
	}
	var rel releaseFace
	if err := json.Unmarshal(body, &rel); err != nil {
		fmt.Fprintf(out, "[ocrelease] cannot decode the promote response: %v\n", err)
		return 1
	}
	stamp := ""
	if rel.GAAt != nil {
		stamp = " at " + time.Unix(int64(*rel.GAAt), 0).UTC().Format("2006-01-02 15:04:05Z")
	}
	fmt.Fprintf(out, "%s is GA%s — GA-channel clients now see it as latest\n", rel.releaseLabel(), stamp)
	return 0
}

// doRequest runs one bounded HTTP call and slurps the (size-capped) body.
func doRequest(req *http.Request) (status int, body []byte, err error) {
	client := &http.Client{Timeout: uploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout))
}
