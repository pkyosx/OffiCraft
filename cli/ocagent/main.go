// Command ocagent is a self-contained Go port of officraft's Python
// `agent/oc_agent.py` CLI — the agent-runtime (Plane A) thin shell an agent
// process shells out to for identity-scoped server operations. The motive
// mirrors ocwarden: ship one static binary so agent machines need no Python
// install.
//
// SCOPE — Plane A, minimum agent-CLI surface only. This binary ports ONLY the
// subcommands an agent genuinely needs on the CLI: the two that MCP cannot cover
// (listen — a persistent SSE downlink; context-report — a Claude Code statusLine
// shell command). The presence phase report is NOT a CLI subcommand — the agent
// reports phase=waking through the MCP `set_member_presence` tool instead (same
// route, same recycle loop-break; pure transport swap). The other agent-initiated
// operations (roster / chat-send / bootstrap self-check) are likewise agent-driven
// requests that go over MCP instead and were deliberately NOT ported. This binary
// also does NOT port Plane B (spawn / reconcile): that is the warden's job (see
// ocwarden), and its hardest surface — tmux screen-scraping — is out of scope.
//
// PHASE STATUS. Phase 0 (scaffold) landed the module skeleton and the shared
// config/http seam ported from ocwarden. context-report AND listen (the persistent
// SSE downlink — Phase 4) are implemented and dispatched; the spawn shim execs
// THIS binary (the Python CLI it originally shadowed is retired — git tag
// py-final).
//
// Design note: like ocwarden this is stdlib-only, zero third-party deps. The
// network seam is the injectable httpClient interface — tests point Config.Base
// at an httptest.Server, driving the whole chain with zero real network.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// planeASubcommands is the minimum agent-CLI surface this binary covers: the two
// subcommands MCP cannot do (listen — persistent SSE; context-report — statusLine
// shell command). presence / roster / chat-send / bootstrap are intentionally
// absent — they are agent-initiated requests that go over MCP (presence via the
// `set_member_presence` tool), not the CLI. Kept as data so the usage text and the
// dispatch stay in one place.
var planeASubcommands = []struct{ name, help string }{
	{"listen", "hold the SSE downlink: chat (refetch) + work wakes"},
	{"context-report", "statusLine reporter: stdin statusLine JSON → POST /api/agent/context"},
	{"suicide", "self-terminate: kill my own tmux session (OC_SESSION) → SSE drops → offline"},
	{"download", "fetch a chat attachment blob to a local file (streaming; --out <dir>)"},
	{"upload", "stream a local file into the attachment store (prints the att id; --mime <type>)"},
}

func usage(out io.Writer) {
	fmt.Fprintln(out, "usage: ocagent <subcommand> [flags]")
	fmt.Fprintln(out, "  officraft agent-runtime (Plane A) thin shell.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "subcommands:")
	for _, s := range planeASubcommands {
		fmt.Fprintf(out, "  %-15s %s\n", s.name, s.help)
	}
}

// realMain is the testable entrypoint: argv WITHOUT the program name, an env
// accessor, the stdin source (for chat-send's stdin body), and the output sink.
// Returns the process exit code.
func realMain(argv []string, env func(string) string, in io.Reader, out io.Writer) int {
	if len(argv) == 0 {
		usage(out)
		return 2
	}
	cmd, rest := argv[0], argv[1:]
	cfg := loadConfig(env)

	switch cmd {
	case "context-report":
		// No flags (mirrors argparse: sub.add_parser("context-report") has none).
		fs := flag.NewFlagSet("ocagent context-report", flag.ContinueOnError)
		fs.SetOutput(out)
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		// now in fractional unix seconds — mirrors Python's time.time() (the throttle
		// stamp stores/compares this float).
		now := float64(time.Now().UnixNano()) / 1e9
		return cmdContextReport(defaultHTTPClient(), cfg, env, now, in, out)

	case "listen":
		// The canonical SSE downlink: hold GET /api/events open (⇒ server-projected
		// online), turn each delta into a wake (chat refetch / member hooks / work
		// wake), and self-exit when this agent's own tmux session disappears. --once
		// does a single connect (the test hook, mirrors argparse). See listen*.go.
		fs := flag.NewFlagSet("ocagent listen", flag.ContinueOnError)
		fs.SetOutput(out)
		once := fs.Bool("once", false, "do a single connect then return (test/diagnostic hook)")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		return cmdListen(cfg, env, *once, out)

	case "suicide":
		// The graceful self-kill: kill my own tmux session (OC_SESSION on
		// OC_TMUX_SOCKET) so the SSE drops → server derives offline. No flags. The
		// winddown/recycle hooks invoke this after reporting phase=stopped; it is also
		// a documented manual lever. See suicide.go.
		fs := flag.NewFlagSet("ocagent suicide", flag.ContinueOnError)
		fs.SetOutput(out)
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		return cmdSuicide(cfg, env, out)

	case "download":
		// The receive side of chat attachments: stream GET /api/chat/attachment/<id>
		// (a route excluded from the MCP surface — a binary blob, not a tool result)
		// to a local file under the agent's workdir, so the agent can read/unzip
		// what another member sent it. Success prints ONLY the landed absolute path
		// on stdout; diagnostics + errors go to stderr with distinct exit codes
		// (see download.go). See download.go for the streaming/naming contract.
		fs := flag.NewFlagSet("ocagent download", flag.ContinueOnError)
		fs.SetOutput(out)
		outDir := fs.String("out", "", "destination directory (default: tmp/attachments/ under the agent workdir)")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		// stdlib flag stops at the first positional, so `download <id> --out <dir>`
		// (the documented order) leaves --out unparsed — re-parse what follows the
		// id so the flag works on either side of the positional.
		args := fs.Args()
		if len(args) >= 1 {
			if err := fs.Parse(args[1:]); err != nil {
				return 2
			}
		}
		if len(args) < 1 || fs.NArg() != 0 {
			fmt.Fprintln(out, "[ocagent] download: exactly one <attachment-id> argument is required")
			fmt.Fprintln(out, "usage: ocagent download <attachment-id> [--out <dir>]")
			return 2
		}
		return cmdDownload(newStreamingClient(), cfg, args[0], *outDir, out, os.Stderr)

	case "upload":
		// The send side of chat attachments: stream a local file's bytes to
		// POST /api/chat/attachments (MCP-excluded — a binary ingest, not a tool)
		// and print the minted attachment id + light-ref JSON, so the agent posts
		// the message with a light {id} ref instead of dragging base64 through
		// its own context. See upload.go for the streaming/exit-code contract.
		fs := flag.NewFlagSet("ocagent upload", flag.ContinueOnError)
		fs.SetOutput(out)
		mimeType := fs.String("mime", "", "declared media type (default: server-side sniff)")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		// stdlib flag stops at the first positional, so `upload <path> --mime <t>`
		// (the documented order) leaves --mime unparsed — re-parse what follows
		// the path so the flag works on either side of the positional.
		args := fs.Args()
		if len(args) >= 1 {
			if err := fs.Parse(args[1:]); err != nil {
				return 2
			}
		}
		if len(args) < 1 || fs.NArg() != 0 {
			fmt.Fprintln(out, "[ocagent] upload: exactly one <path> argument is required")
			fmt.Fprintln(out, "usage: ocagent upload <path> [--mime <type>]")
			return 2
		}
		return cmdUpload(newStreamingClient(), cfg, args[0], *mimeType, out, os.Stderr)

	case "version", "--version", "-v":
		// Print WHICH build this is (git sha/time/dirty when stamped + always a
		// content self-hash) so a human can tell an eva self-updated binary apart from
		// the committed bin/ artifact. Deliberately NOT part of usage()/--help so CI's
		// committed-prebuilt parity dryrun (bin/ci.sh 7d, which diffs --help) is unaffected.
		return cmdVersion(out)

	case "-h", "--help", "help":
		usage(out)
		return 0

	default:
		fmt.Fprintf(out, "[ocagent] unknown subcommand %q\n\n", cmd)
		usage(out)
		return 2
	}
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
}
