// Command ocagent is the agent-runtime (Plane A) CLI. Plane B (spawn /
// reconcile) is ocwarden's. Agent-initiated requests (presence, roster,
// chat-send, bootstrap) deliberately go over MCP, not this CLI.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// buildSHA is set only by bin/build-bindist (-X main.buildSHA)
// and is empty in every other build; empty means "no stamp", never a substitute.
// Never derive it at runtime: a long-lived listener would report the sha on
// disk NOW, not the build it is running.
var buildSHA string

var planeASubcommands = []struct{ name, help string }{
	{"listen", "hold the SSE downlink: chat (refetch) + work wakes"},
	{"context-report", "statusLine reporter: stdin statusLine JSON → POST /api/agent/context"},
	{"suicide", "self-terminate: kill my own tmux session (OC_SESSION) → SSE drops → offline"},
	{"download", "fetch a chat attachment blob to a local file (streaming; --out <dir>)"},
	{"upload", "stream a local file into the attachment store (prints the att id; --mime <type>)"},
	{"diff", "print a compare-screen URL for two attachment ids / document versions (--external mints a no-login link)"},
	// guard-bash / guard-permission are wired by cli/ocwarden/spawn.go into every
	// member's settings.json hooks, not run by hand; listed so an agent they
	// refused can find them in --help.
	{"guard-bash", "PreToolUse hook: refuse the removal shapes that stall a headless member"},
	{"guard-permission", "PermissionRequest hook: refuse every confirmation prompt nobody is here to answer"},
	{"version", "print this build's identity: build.sha, VCS stamp when present, self-hash"},
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

func realMain(argv []string, env func(string) string, in io.Reader, out io.Writer) int {
	if len(argv) == 0 {
		usage(out)
		return 2
	}
	cmd, rest := argv[0], argv[1:]
	cfg := loadConfig(env)

	switch cmd {
	case "context-report":
		fs := flag.NewFlagSet("ocagent context-report", flag.ContinueOnError)
		fs.SetOutput(out)
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		now := float64(time.Now().UnixNano()) / 1e9
		return cmdContextReport(defaultHTTPClient(), cfg, env, now, in, out, os.Stderr)

	case "listen":
		return cmdListen(rest, cfg, env, out, runListen, nil)

	case "suicide":
		fs := flag.NewFlagSet("ocagent suicide", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { suicideUsage(out) }
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		return cmdSuicide(cfg, env, out)

	case "download":
		fs := flag.NewFlagSet("ocagent download", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { downloadUsage(out) }
		outDir := fs.String("out", "", "destination directory (default: tmp/attachments/ under the agent workdir)")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
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
		return cmdDownload(newNoDeadlineClient(), cfg, args[0], *outDir, out, os.Stderr)

	case "upload":
		fs := flag.NewFlagSet("ocagent upload", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { uploadUsage(out) }
		mimeType := fs.String("mime", "", "declared media type (default: server-side sniff)")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
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
		return cmdUpload(newNoDeadlineClient(), cfg, args[0], *mimeType, out, os.Stderr)

	case "diff":
		fs := flag.NewFlagSet("ocagent diff", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { diffUsage(out) }
		labelBefore := fs.String("label-before", "", "column heading for the before side (default: the screen's own)")
		labelAfter := fs.String("label-after", "", "column heading for the after side (default: the screen's own)")
		external := fs.Bool("external", false, "mint the server-signed link that opens with no login (no expiry; ends only when its signing key is removed)")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		args := fs.Args()
		if len(args) >= 2 {
			if err := fs.Parse(args[2:]); err != nil {
				return 2
			}
		}
		if len(args) < 2 || fs.NArg() != 0 {
			fmt.Fprintln(out, "[ocagent] diff: exactly two arguments are required: <before> <after>")
			diffUsage(out)
			return 2
		}
		return cmdDiff(newNoDeadlineClient(), cfg, args[0], args[1],
			*labelBefore, *labelAfter, *external, out, os.Stderr)

	case "guard-bash":
		fs := flag.NewFlagSet("ocagent guard-bash", flag.ContinueOnError)
		fs.SetOutput(out)
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		return cmdGuardBash(in, out)

	case "guard-permission":
		fs := flag.NewFlagSet("ocagent guard-permission", flag.ContinueOnError)
		fs.SetOutput(out)
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		return cmdGuardPermission(in, out)

	case "version", "--version", "-v":
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
