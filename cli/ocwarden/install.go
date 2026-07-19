// `ocwarden install` — the one-key warden installer (the Go replacement of the
// retired flip-era bash bin/warden-install, now the SOLE installer). It
// idempotently installs and starts THIS machine's execution-plane warden launchd
// job (canonical label com.officraft.ocwarden) in six steps: resolve
// identity/paths, write the exec-warden token file (0600, atomic), render the
// launchd plist with real per-machine paths, launchctl bootout→poll-until-gone→
// bootstrap→kickstart under the EXACT label, then health-verify the job came up
// and STAYS up. The poll between bootout and bootstrap is load-bearing: bootout is
// ASYNC, and bootstrapping while the old registration lingers fails with
// "Bootstrap failed: 5: Input/output error" — the exact non-idempotence that broke
// re-installs in the field. bin/ocserver's drop_and_load is the same pattern on
// the server side; keep the two in sync.
//
// SEAM DESIGN (mirrors main.go's CmdRunner): every side effect — launchctl/plutil
// subprocess, file mkdir/write/rename/chmod/stat, and the settle-window sleep — goes
// through the injectable sysOps struct. Real runs use realSysOps() (os/exec + os);
// tests inject fakes so unit tests and CI (gate 7 = gofmt+vet+build) NEVER touch
// launchctl or the live machine. WARDEN_INSTALL_DRYRUN=1 is the dry-run seam
// (byte-parity with the bash installer's env of the same name): it prints every
// step's intent and mutates nothing.
//
// GUARDRAILS (ported verbatim from the bash installer):
//   - The ONLY process action is launchctl by the EXACT label
//     com.officraft.ocwarden. NEVER pkill / pattern-kill / killall.
//   - Tokfile is mode 0600, written to a fresh temp then atomically renamed.
//   - Every step is idempotent (reinstall boots out the old instance first).
//   - The binary is the committed prebuilt bin/ocwarden — install does NOT rebuild
//     (a fresh machine with no Go toolchain must still be able to install).
package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// wardenLabel is the CANONICAL execution-plane warden launchd label. It is the ONLY
// label install/teardown ever act on (bootout/bootstrap/kickstart) — never a
// pattern, never the python daemons' labels. Byte-identical to the bash installer's
// readonly LABEL and to the committed plist template's <Label>.
const wardenLabel = "com.officraft.ocwarden"

// dryRunEnv is the dry-run toggle env key, kept byte-identical to the bash
// installer so operators use the same incantation for either implementation.
const dryRunEnv = "WARDEN_INSTALL_DRYRUN"

// ocBaseShape asserts OC_BASE is http(s)://host[:port] with no whitespace or
// XML-special chars BEFORE it is interpolated into the plist XML (defence against a
// malformed / injection-y value slipping into the rendered document). Ported from
// the bash installer's `^https?://[^[:space:]\"\'\<\>\&]+$`.
var ocBaseShape = regexp.MustCompile(`^https?://[^\s"'<>&]+$`)

// launchctlPIDRe extracts the `pid = N` line from `launchctl print`.
var launchctlPIDRe = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)`)

// ---------------------------------------------------------------------------
// sysOps — the injectable side-effect seam (mirrors main.go's CmdRunner idea,
// widened to the filesystem + sleep so install/teardown are fully testable and CI
// never touches launchctl or the live machine).
// ---------------------------------------------------------------------------

type sysOps struct {
	run       func(name string, args ...string) (string, error) // launchctl / plutil
	mkdirAll  func(path string, perm os.FileMode) error
	writeFile func(path string, data []byte, perm os.FileMode) error
	readFile  func(path string) ([]byte, error) // copy-self source + guard tokfile read
	rename    func(oldpath, newpath string) error
	remove    func(path string) error
	chmod     func(path string, mode os.FileMode) error
	statMode  func(path string) (os.FileMode, error)
	sleep     func(time.Duration)
}

// realSysOps wires the seam to the real OS. The runner reuses execRunner (main.go)
// with a generous timeout: launchctl bootstrap/print are quick, but the health
// verify makes many one-shot calls — each individual call is well under this bound.
func realSysOps() sysOps {
	r := execRunner{timeout: 30 * time.Second}
	return sysOps{
		run:       r.Run,
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		rename:    os.Rename,
		remove:    os.Remove,
		chmod:     os.Chmod,
		statMode: func(p string) (os.FileMode, error) {
			fi, err := os.Stat(p)
			if err != nil {
				return 0, err
			}
			return fi.Mode(), nil
		},
		sleep: time.Sleep,
	}
}

// installer carries the shared state for both install and teardown: where to log,
// whether this is a dry run, and the side-effect seam.
type installer struct {
	out    io.Writer
	dryRun bool
	force  bool // --force: override the one-warden-per-machine guard
	sys    sysOps

	// resolveClaude is the install-time claude RESOLUTION seam (nil = skip, no
	// stamp — legacy fixtures/tests are untouched). It returns (claudeBin,
	// plistPATH): claudeBin is the absolute claude executable to stamp into the
	// warden plist as OC_CLAUDE_BIN ("" = unresolved → warn with guidance);
	// plistPATH is a non-default PATH to stamp when claude only runs under the
	// installer's richer PATH (version-manager shim / shebang interpreter — see
	// resolveClaudeForInstall; "" keeps the historical minimal wardenPlistPATH).
	// WHY at install time: `ocwarden install` runs in an env that can actually
	// FIND claude (the operator's interactive shell, or a serve process whose own
	// plist carries OC_CLAUDE_BIN from `bin/ocserver install` via bootstrap-here),
	// while the launchd warden it installs runs under a minimal env where the
	// runtime resolveClaudeBin fallbacks (LookPath / common dirs) miss
	// version-manager installs (asdf/nvm/volta) → claude_bin_unresolved on every
	// spawn. The stamp makes runtime priority ① (OC_CLAUDE_BIN) deterministic.
	resolveClaude func() (claudeBin, plistPATH string)

	// agentGet + agentProbe are the ocagent DOWNLOAD seam (default install path): fetch
	// the committed prebuilt ocagent from the server (GET /api/agent/binary) and
	// verify-before-write it, so a remote/empty machine with NO repo and NO OC_AGENT_BIN
	// still gets a working ocagent. Reuse of selfupdate.go's getter + probe shapes.
	// agentGet GETs a path → (status, body, transport-error). agentProbe must EXEC the
	// freshly downloaded binary and return nil ONLY if it runs and exits 0 (anti-suicide
	// verify-before-swap: a truncated / wrong-arch download fails here). Both are wired by
	// installCmd (realSysOps) and injected as fakes in tests; NEVER touched under DRYRUN
	// or when OC_AGENT_BIN provides a local override (dev/in-tree).
	agentGet   getter
	agentProbe func(bin string) error
}

func (i *installer) logf(format string, a ...any) {
	fmt.Fprintf(i.out, "[ocwarden install] "+format+"\n", a...)
}
func (i *installer) errf(format string, a ...any) {
	fmt.Fprintf(i.out, "[ocwarden install] FATAL: "+format+"\n", a...)
}

// ---------------------------------------------------------------------------
// path + identity resolution (bash installer step 1) — PURE, so it is unit-tested
// without touching the machine.
// ---------------------------------------------------------------------------

// wardenPaths is the fully-resolved set of per-machine paths + identity the six
// install steps operate on.
type wardenPaths struct {
	root string // per-machine data root = $HOME/.officraft (plist WorkingDirectory)
	home string
	// namespace is the validated OC_NAMESPACE ("" = the main instance). label is
	// the launchd label derived from it (wardenLabelFor); the zero value falls
	// back to the canonical wardenLabel (labelOrDefault) so hand-built fixtures
	// keep their historical meaning.
	namespace string
	label     string
	srcExe    string // the running binary to copy from (symlinks already resolved)
	ocBase    string
	ocToken   string
	ocID      string // optional display id; server derives from token sub if empty
	tokfile   string
	laDir     string
	plistPath string
	logDir    string
	binPath   string // STABLE home install target = $HOME/.officraft/warden/ocwarden
	guiDomain string // gui/<uid>
	// ocAgentSrc is an OPTIONAL install-time LOCAL OVERRIDE (from OC_AGENT_BIN env) for
	// the ocagent binary. When set, installOcAgent copies THIS local file to ocAgentBin
	// (dev/test/in-tree, no server needed). When EMPTY (the DEFAULT + production path),
	// installOcAgent instead DOWNLOADS ocagent from the server (GET /api/agent/binary) →
	// so a remote/empty machine with NO repo and NO local path still gets a working
	// ocagent. Either way the warden finds ocagent as its own sibling (resolveOcAgentBin),
	// and that sibling only exists because install put it there.
	ocAgentSrc string
	// ocAgentBin is the STABLE home target = $HOME/.officraft/warden/ocagent (sibling of
	// ocwarden) that installOcAgent writes (whether via local-copy or download). NOT
	// stamped into any env/plist — the runtime warden discovers it by looking next to its
	// own executable (resolveOcAgentBin).
	ocAgentBin string
	// claudeBin is the claude executable resolved AT INSTALL TIME (installer
	// seam resolveClaude; "" = unresolved). Non-empty → stamped into the plist
	// EnvironmentVariables as OC_CLAUDE_BIN so the launchd warden's runtime
	// resolveClaudeBin priority ① hits deterministically (the launchd minimal
	// env cannot re-discover a version-manager claude on its own).
	claudeBin string
	// plistPATH overrides the plist's PATH env value ("" = the historical
	// minimal wardenPlistPATH — byte-identical render). Set when the resolved
	// claude only runs under the installer's richer PATH (a version-manager
	// shim or `#!/usr/bin/env node` shebang needs its interpreter on PATH).
	plistPATH string
}

// labelOrDefault is the launchd label every install/teardown/launchctl step
// acts on: the namespace-derived p.label when resolved, else the canonical
// wardenLabel (empty namespace / zero-value fixtures — byte-identical either way).
func (p wardenPaths) labelOrDefault() string {
	if p.label != "" {
		return p.label
	}
	return wardenLabel
}

// resolvePaths reads the env contract (OC_BASE / OC_TOKEN / OC_ID — DEFINED to align
// with the server's boot_command) and derives every per-machine path. The install is
// SELF-CONTAINED: regardless of where the running binary (`exe`) currently lives (a
// clone's bin/, /tmp/ocwarden, ./ocwarden, …), the durable warden runs from a STABLE
// per-machine home: root = $HOME/.officraft, binPath = $HOME/.officraft/warden/ocwarden,
// logDir = $HOME/.officraft/warden/log, tokfile = $HOME/.officraft/warden/exec-warden.tok. `exe`
// is retained as srcExe: the source the installer copies to binPath. OC_TOKEN is
// required; OC_BASE defaults + is shape-asserted; OC_ID is optional.
func resolvePaths(env func(string) string, exe string, uid int) (wardenPaths, error) {
	home := env("HOME")
	if home == "" {
		return wardenPaths{}, errors.New("HOME must be set")
	}
	// Namespace (OC_NAMESPACE, "" = main instance) keys every per-instance host
	// resource; an invalid value is refused before anything is derived.
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return wardenPaths{}, err
	}
	label := wardenLabelFor(ns)
	// Data root is the stable per-machine home dir, NOT a repo/clone location: the
	// installed warden must survive deletion of the copy `exe` was launched from.
	root := officraftRootFor(home, ns)

	ocBase := env("OC_BASE")
	if ocBase == "" {
		ocBase = defaultBase
	}
	ocBase = strings.TrimRight(ocBase, "/") // mirror loadConfig
	if !ocBaseShape.MatchString(ocBase) {
		return wardenPaths{}, fmt.Errorf("OC_BASE must be http(s)://host[:port] with no whitespace/XML-special chars, got: %s", ocBase)
	}

	ocToken := env("OC_TOKEN")
	if ocToken == "" {
		return wardenPaths{}, errors.New("OC_TOKEN is required (the exec-warden member token; NOT the telemetry warden's). Usage: OC_BASE=<base> OC_TOKEN=<jwt> [OC_ID=<id>] ocwarden install")
	}

	// OC_AGENT_BIN (install-time) is an OPTIONAL LOCAL OVERRIDE naming a source ocagent to
	// copy into the home bin (dev/test/in-tree). When UNSET (the DEFAULT + production
	// path) install DOWNLOADS ocagent from the server instead — zero repo dependency, so
	// a remote/empty machine still gets a working ocagent. When SET it must be an ABSOLUTE
	// path with no whitespace (installOcAgent's read source — a relative or
	// whitespace-laden path is almost certainly a mistake, caught here before any
	// mutation). It is NOT interpolated anywhere (the warden discovers the copied binary as
	// a runtime sibling), so this is path hygiene only.
	ocAgentSrc := strings.TrimSpace(env("OC_AGENT_BIN"))
	if ocAgentSrc != "" {
		if !filepath.IsAbs(ocAgentSrc) || strings.ContainsAny(ocAgentSrc, " \t\n\r") {
			return wardenPaths{}, fmt.Errorf("OC_AGENT_BIN must be an absolute path with no whitespace, got: %s", ocAgentSrc)
		}
	}

	return wardenPaths{
		root:       root,
		home:       home,
		namespace:  ns,
		label:      label,
		srcExe:     exe,
		ocBase:     ocBase,
		ocToken:    ocToken,
		ocID:       env("OC_ID"),
		tokfile:    tokfileFor(home, ns), // $HOME/.officraft[-ns]/warden/exec-warden.tok
		laDir:      filepath.Join(home, "Library", "LaunchAgents"),
		plistPath:  filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		logDir:     filepath.Join(root, "warden", "log"),      // $HOME/.officraft/warden/log
		binPath:    filepath.Join(root, "warden", "ocwarden"), // $HOME/.officraft/warden/ocwarden
		guiDomain:  fmt.Sprintf("gui/%d", uid),
		ocAgentSrc: ocAgentSrc,
		ocAgentBin: filepath.Join(root, "warden", "ocagent"), // sibling of ocwarden (home copy)
	}, nil
}

// ---------------------------------------------------------------------------
// plist render (bash installer step 4) — the AUTHORITATIVE renderer. Inlined
// (not go:embed) so the rendered output is byte-parity with the bash installer's
// render_plist heredoc; the committed deploy/*.plist reference template documents
// the same shape for reviewers.
// ---------------------------------------------------------------------------

// wardenPlistPATH is the historical minimal launchd PATH the plist stamps by
// default. It deliberately lacks ~/.local/bin and every version-manager shim
// dir — which is exactly why the launchd warden cannot re-discover claude at
// runtime and the install-time OC_CLAUDE_BIN stamp exists.
const wardenPlistPATH = "/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// plistTemplate mirrors the retired bash installer's render_plist heredoc exactly: the same
// keys, ProgramArguments (BIN + "run"), EnvironmentVariables (PATH/OC_BASE/HOME/
// OC_WARDEN_TOKFILE), RunAtLoad/KeepAlive/ThrottleInterval, and the ocwarden.*
// log paths. %[1]s=ROOT %[2]s=BIN %[3]s=OC_BASE %[4]s=HOME %[5]s=TOKFILE %[6]s=LOGDIR
// %[7]s=LABEL %[8]s=optional extra env lines (OC_NAMESPACE / OC_CLAUDE_BIN; "" for
// the main instance with no resolved claude — that render stays byte-identical to
// the historical output) %[9]s=PATH value (wardenPlistPATH unless overridden).
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- RENDERED by ocwarden install for ROOT=%[1]s — do not edit by hand; re-run the installer. -->
<plist version="1.0">
<dict>
    <key>Label</key><string>%[7]s</string>
    <key>ProgramArguments</key>
    <array><string>%[2]s</string><string>run</string></array>
    <key>WorkingDirectory</key><string>%[1]s</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key><string>%[9]s</string>
        <key>OC_BASE</key><string>%[3]s</string>
        <key>HOME</key><string>%[4]s</string>
        <key>OC_WARDEN_TOKFILE</key><string>%[5]s</string>%[8]s
    </dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>%[6]s/ocwarden.out.log</string>
    <key>StandardErrorPath</key><string>%[6]s/ocwarden.err.log</string>
</dict>
</plist>
`

// renderPlist substitutes the real per-machine paths into the template. It emits NO
// OC_AGENT_BIN env: the warden finds ocagent as its own sibling at runtime
// (resolveOcAgentBin), so the plist stays free of the ocagent path entirely.
// OC_NAMESPACE is stamped ONLY for a non-empty namespace, OC_CLAUDE_BIN ONLY when
// the install resolved a claude executable, and PATH deviates from the minimal
// wardenPlistPATH ONLY when plistPATH overrides it (conditional renders — the
// historical zero-extras plist stays byte-identical to the pre-stamp output).
func renderPlist(p wardenPaths) string {
	extraEnv := ""
	if p.namespace != "" {
		extraEnv += "\n        <key>OC_NAMESPACE</key><string>" + p.namespace + "</string>"
	}
	if p.claudeBin != "" {
		extraEnv += "\n        <key>OC_CLAUDE_BIN</key><string>" + xmlEscape(p.claudeBin) + "</string>"
	}
	pathVal := p.plistPATH
	if pathVal == "" {
		pathVal = wardenPlistPATH
	}
	return fmt.Sprintf(plistTemplate, p.root, p.binPath, p.ocBase, p.home, p.tokfile, p.logDir, p.labelOrDefault(), extraEnv, xmlEscape(pathVal))
}

// xmlEscape escapes the five XML-special characters for a plist <string> value.
// claudeBin is already shape-refused when it carries any of them (stampableClaude),
// so this is defence-in-depth; PATH values legitimately vary and get real escaping.
func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	).Replace(s)
}

// xmlWellFormed asserts the rendered plist parses as XML (the machine-independent
// equivalent of the bash installer's `plutil -lint`: a malformed render fails here
// in BOTH dry-run and live, so pre-land verification actually catches it). Live
// installs additionally run the real `plutil -lint` on the written file for parity.
func xmlWellFormed(doc string) error {
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ---------------------------------------------------------------------------
// install-time claude resolution (the OC_CLAUDE_BIN stamp)
// ---------------------------------------------------------------------------

// stampableClaude asserts a resolved claude path is safe to interpolate into the
// plist XML and sane as an exec target: absolute, no whitespace, no XML-special
// chars (same hygiene class as the OC_AGENT_BIN / OC_BASE shape checks).
func stampableClaude(p string) bool {
	return filepath.IsAbs(p) && !strings.ContainsAny(p, " \t\n\r\"'<>&")
}

// resolveClaudeForInstall resolves the claude CLI in the INSTALLER's environment
// and decides what to stamp into the warden plist. This is the product fix for
// "launchd warden 永遠 claude_bin_unresolved": the runtime resolveClaudeBin's
// fallbacks (LookPath under the minimal launchd PATH, common install dirs) all
// miss a version-manager claude (asdf/nvm/volta shims), so the ONE moment the
// path is discoverable is install time — `ocwarden install` runs either in the
// operator's interactive shell (rich PATH) or under a serve process whose own
// plist carries OC_CLAUDE_BIN (stamped by `bin/ocserver install`, forwarded by
// bootstrap-here's env passthrough).
//
//	lookup — returns the claude candidate ("" = none). Production binds
//	         resolveClaudeBin(env): OC_CLAUDE_BIN override → LookPath under the
//	         installer's PATH → common install dirs.
//	probe  — execs `<bin> --version` under a GIVEN (PATH, HOME) env; nil error =
//	         it runs. Used to detect the shim/shebang trap: an asdf shim or an
//	         `#!/usr/bin/env node` launcher needs its manager/interpreter on
//	         PATH, so an absolute path alone can still die under launchd's
//	         minimal PATH.
//
// Returns (claudeBin, plistPATH):
//   - ("", "")            — claude truly absent (or unstampable); caller prints
//     the human-readable guidance and stamps nothing.
//   - (bin, "")           — bin runs under the minimal wardenPlistPATH → stamp
//     OC_CLAUDE_BIN only (historical PATH kept).
//   - (bin, installerPATH) — bin only runs under the installer's PATH (shim) →
//     stamp OC_CLAUDE_BIN AND carry the installer's PATH into the plist so the
//     shim can find its manager/interpreter at runtime.
//   - (bin, "") with a warning — bin failed BOTH probes; stamp best-effort (the
//     spawn-time guard will surface a precise claude_bin_unresolved otherwise,
//     and a claude that needs more than PATH+HOME may still run under launchd).
func resolveClaudeForInstall(env func(string) string, lookup func() string,
	probe func(bin, pathEnv, home string) error, logf func(string, ...any)) (string, string) {
	cand := lookup()
	if cand == "" {
		return "", ""
	}
	if !stampableClaude(cand) {
		logf("WARN: resolved claude path %q is not stampable (must be absolute, no whitespace/XML-special chars) — not stamping OC_CLAUDE_BIN", cand)
		return "", ""
	}
	if probe == nil {
		return cand, ""
	}
	home := env("HOME")
	if probe(cand, wardenPlistPATH, home) == nil {
		return cand, "" // runs under the minimal launchd PATH → stamp path only
	}
	if userPATH := env("PATH"); userPATH != "" && probe(cand, userPATH, home) == nil {
		logf("claude at %s needs the installer's PATH to run (version-manager shim / env-shebang) — stamping the full installer PATH into the warden plist alongside OC_CLAUDE_BIN", cand)
		return cand, userPATH
	}
	logf("WARN: claude at %s failed `--version` under both the minimal launchd PATH and the installer PATH — stamping OC_CLAUDE_BIN best-effort; spawns may still fail (check that claude runs headless)", cand)
	return cand, ""
}

// claudeProbeBudget bounds one `claude --version` probe. Generous: a cold Node
// CLI can take seconds; a wedged shim must not hang the install forever.
const claudeProbeBudget = 20 * time.Second

// realClaudeProbe execs `<bin> --version` under EXACTLY the given PATH+HOME (the
// same env shape the launchd plist grants), answering "would this claude run
// under the warden's runtime env". Read-only: no files, no launchctl.
func realClaudeProbe(bin, pathEnv, home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), claudeProbeBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = []string{"PATH=" + pathEnv, "HOME=" + home}
	return cmd.Run()
}

// ---------------------------------------------------------------------------
// the six steps
// ---------------------------------------------------------------------------

// guard enforces one warden per machine. "already installed" = an existing tokfile at
// p.tokfile. Its owning machine id is the `sub` claim of the EXISTING token (decoded,
// NOT signature-verified — the installer has no secret; jwtSub base64-decodes the
// middle segment only). The new install is THE SAME machine when the existing sub
// matches EITHER the `sub` of the new OC_TOKEN (the authoritative server-minted
// machine id — like compared with like) OR the optional OC_ID display id. Matching
// token-sub-to-token-sub FIRST is load-bearing for re-run idempotence: a half-install
// leaves a tokfile whose sub equals any re-minted token's sub, and a stray/display
// OC_ID in the caller's env must NOT make the guard mistake the machine re-installing
// ITSELF for a foreign warden (that false refusal broke half-install re-runs). If a
// warden for a genuinely DIFFERENT machine is already installed, REFUSE and mutate
// nothing (operator must `ocwarden teardown` or pass --force); no existing tokfile is
// a fresh install. This runs BEFORE any mutation and refuses identically under DRYRUN
// (the tokfile read is a non-mutating probe).
func (i *installer) guard(p wardenPaths) error {
	if i.force {
		i.logf("--force: skipping one-warden-per-machine guard")
		return nil
	}
	raw, err := i.sys.readFile(p.tokfile)
	if err != nil {
		return nil // no existing tokfile (or unreadable) → fresh install
	}
	existing := jwtSub(strings.TrimSpace(string(raw)))
	if existing == "" {
		return nil // can't identify the existing warden → don't block a re-provision
	}
	if newSub := jwtSub(p.ocToken); existing == newSub || (p.ocID != "" && existing == p.ocID) {
		i.logf("guard: existing warden is the same machine (%s) — idempotent re-provision", existing)
		return nil
	}
	return fmt.Errorf("refusing: a warden for machine %s is already installed on this box; run 'ocwarden teardown' first, or pass --force to replace", existing)
}

// copyBinary makes the install self-contained: it copies the running binary
// (p.srcExe, symlinks already resolved by installCmd) to the STABLE home target
// p.binPath ($HOME/.officraft/warden/ocwarden) mode 0755 via a fresh temp + atomic
// rename, so the durable warden runs from home and survives deletion of the temp/clone
// copy it was launched from. A no-op when srcExe already IS binPath (re-run from the
// installed location). DRYRUN logs intent and copies nothing.
func (i *installer) copyBinary(p wardenPaths) error {
	dir := filepath.Dir(p.binPath)
	if p.srcExe == p.binPath {
		i.logf("running binary is already the installed home binary (%s); skipping self-copy", p.binPath)
		return nil
	}
	if i.dryRun {
		i.logf("DRYRUN would: mkdir -p %s; copy %s -> a 0755 temp then atomic rename -> %s", dir, p.srcExe, p.binPath)
		return nil
	}
	if err := i.sys.mkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir bin dir %s: %w", dir, err)
	}
	data, err := i.sys.readFile(p.srcExe)
	if err != nil {
		return fmt.Errorf("read own binary %s: %w", p.srcExe, err)
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".ocwarden.%d", os.Getpid()))
	if err := i.sys.writeFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("write temp binary %s: %w", tmp, err)
	}
	// Explicit chmod: WriteFile's mode is umask-masked; re-assert 0755 so the copy is
	// executable regardless of the caller's umask.
	if err := i.sys.chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod temp binary %s: %w", tmp, err)
	}
	if err := i.sys.rename(tmp, p.binPath); err != nil {
		return fmt.Errorf("atomic rename binary -> %s: %w", p.binPath, err)
	}
	i.logf("installed binary (0755): %s", p.binPath)
	return nil
}

// installOcAgent completes the SELF-CONTAINED install by putting a working ocagent at
// the stable home sibling p.ocAgentBin ($HOME/.officraft/warden/ocagent) mode 0755,
// so the spawn shim execs a home-owned binary that survives deletion of the run-clone
// it came from. It has TWO sources:
//
//   - DEFAULT (production, OC_AGENT_BIN unset): DOWNLOAD the committed prebuilt ocagent
//     from the server (GET /api/agent/binary, PUBLIC) via i.agentGet, verify-before-write
//     it (i.agentProbe: the download must EXEC + exit 0, the same anti-suicide guard the
//     self-updater uses), then atomic-write it. ZERO repo dependency — a remote/empty
//     machine with no local ocagent path still gets a working binary. This fixes the
//     high-sev bug where the shim pointed at a dead path on machines that had no repo.
//   - OVERRIDE (dev/test/in-tree, OC_AGENT_BIN=<abs path>): copy that LOCAL file instead
//     (no server needed), preserving the old copy-from-local behaviour for CI/dev.
//
// A no-op when p.ocAgentSrc already IS the home target (re-run in place). DRYRUN logs
// intent only. All FS mutation goes through i.sys; the download goes through i.agentGet.
func (i *installer) installOcAgent(p wardenPaths) error {
	if p.ocAgentSrc == p.ocAgentBin && p.ocAgentSrc != "" {
		i.logf("ocagent source is already the installed home binary (%s); skipping self-copy", p.ocAgentBin)
		return nil
	}
	dir := filepath.Dir(p.ocAgentBin)

	// Resolve the bytes to install: local override (copy) or server download.
	var data []byte
	if p.ocAgentSrc != "" {
		if i.dryRun {
			i.logf("DRYRUN would: mkdir -p %s; copy (OC_AGENT_BIN override) %s -> a 0755 temp then atomic rename -> %s", dir, p.ocAgentSrc, p.ocAgentBin)
			return nil
		}
		src, err := i.sys.readFile(p.ocAgentSrc)
		if err != nil {
			return fmt.Errorf("read ocagent source %s: %w", p.ocAgentSrc, err)
		}
		data = src
	} else {
		if i.dryRun {
			i.logf("DRYRUN would: mkdir -p %s; download ocagent from %s%s -> verify-exec -> atomic rename -> %s", dir, p.ocBase, agentBinaryPath, p.ocAgentBin)
			return nil
		}
		if i.agentGet == nil {
			return fmt.Errorf("no ocagent source: OC_AGENT_BIN unset and no download getter wired")
		}
		body, err := i.downloadOcAgent(p)
		if err != nil {
			return err
		}
		data = body
	}

	if err := i.sys.mkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir bin dir %s: %w", dir, err)
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".ocagent.%d", os.Getpid()))
	if err := i.sys.writeFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("write temp ocagent %s: %w", tmp, err)
	}
	// Explicit chmod: WriteFile's mode is umask-masked; re-assert 0755 so the copy is
	// executable regardless of the caller's umask.
	if err := i.sys.chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod temp ocagent %s: %w", tmp, err)
	}
	// VERIFY-BEFORE-SWAP (anti-suicide, download path only — the same guard the
	// self-updater applies): a corrupt / truncated / wrong-arch download must fail to
	// exec here and we bail with NOTHING installed, rather than leaving a dead ocagent at
	// the sibling path that would make every spawned agent exit 127 (deaf/offline). The
	// local-override path skips this: OC_AGENT_BIN is a dev-controlled file, and probing a
	// possibly cross-arch dev binary would add no safety.
	if p.ocAgentSrc == "" && i.agentProbe != nil {
		if err := i.agentProbe(tmp); err != nil {
			_ = i.sys.remove(tmp)
			return fmt.Errorf("downloaded ocagent failed verify — not installing (would brick spawn): %w", err)
		}
	}
	if err := i.sys.rename(tmp, p.ocAgentBin); err != nil {
		return fmt.Errorf("atomic rename ocagent -> %s: %w", p.ocAgentBin, err)
	}
	i.logf("installed ocagent (0755): %s", p.ocAgentBin)
	return nil
}

// downloadOcAgent GETs the committed prebuilt ocagent from the server
// (GET /api/agent/binary, PUBLIC) via i.agentGet and returns its bytes. Any transport
// error, non-200 status, or empty body is a hard failure (install aborts) — better a
// loud install failure than a silently-missing ocagent that deafens every spawn.
func (i *installer) downloadOcAgent(p wardenPaths) ([]byte, error) {
	i.logf("downloading ocagent from %s%s ...", p.ocBase, agentBinaryPath)
	status, body, err := i.agentGet(agentBinaryPath)
	if err != nil {
		return nil, fmt.Errorf("download ocagent from %s%s: %w", p.ocBase, agentBinaryPath, err)
	}
	if status != 200 {
		return nil, fmt.Errorf("download ocagent from %s%s: status %d", p.ocBase, agentBinaryPath, status)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("download ocagent from %s%s: empty body", p.ocBase, agentBinaryPath)
	}
	return body, nil
}

// writeTokfile writes the exec-warden token 0600 via a fresh temp + atomic rename
// (bash installer step 3): the temp is written 0600 and chmod-confirmed, so the
// token is NEVER exposed at loose perms even if $tokfile pre-exists 0644; rename
// replaces atomically (no write-then-chmod window on the live path). No trailing
// newline — the binary's readTokfile trims, but wire-parity with the bash `printf
// '%s'` keeps the file byte-identical.
func (i *installer) writeTokfile(p wardenPaths) error {
	dir := filepath.Dir(p.tokfile)
	if i.dryRun {
		i.logf("DRYRUN would: mkdir -p %s; write <token> to a 0600 temp then atomic rename -> %s", dir, p.tokfile)
		return nil
	}
	if err := i.sys.mkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir tokfile dir %s: %w", dir, err)
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".exec-warden.tok.%d", os.Getpid()))
	if err := i.sys.writeFile(tmp, []byte(p.ocToken), 0o600); err != nil {
		return fmt.Errorf("write temp tokfile %s: %w", tmp, err)
	}
	// Explicit chmod: os.WriteFile's mode is masked by umask, so re-assert 0600 to
	// guarantee the perms regardless of the caller's umask.
	if err := i.sys.chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("chmod temp tokfile %s: %w", tmp, err)
	}
	if err := i.sys.rename(tmp, p.tokfile); err != nil {
		return fmt.Errorf("atomic rename tokfile -> %s: %w", p.tokfile, err)
	}
	mode, err := i.sys.statMode(p.tokfile)
	if err != nil {
		return fmt.Errorf("stat tokfile %s: %w", p.tokfile, err)
	}
	if mode.Perm() != 0o600 {
		return fmt.Errorf("tokfile perms are not 0600: %s (got %o)", p.tokfile, mode.Perm())
	}
	i.logf("wrote tokfile (0600): %s", p.tokfile)
	return nil
}

// writePlist renders the plist, asserts it is well-formed XML, then (live) mkdirs
// LaunchAgents, writes the file, and runs `plutil -lint` for parity (bash step 4).
func (i *installer) writePlist(p wardenPaths) error {
	rendered := renderPlist(p)
	if err := xmlWellFormed(rendered); err != nil {
		return fmt.Errorf("rendered plist is not well-formed XML: %w", err)
	}
	if i.dryRun {
		i.logf("DRYRUN would: mkdir -p %s; render plist -> %s (XML-lint clean)", p.laDir, p.plistPath)
		return nil
	}
	if err := i.sys.mkdirAll(p.laDir, 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents %s: %w", p.laDir, err)
	}
	if err := i.sys.writeFile(p.plistPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write plist %s: %w", p.plistPath, err)
	}
	if _, err := i.sys.run("plutil", "-lint", p.plistPath); err != nil {
		return fmt.Errorf("rendered plist failed plutil -lint %s: %w", p.plistPath, err)
	}
	i.logf("plist rendered + lint-clean: %s", p.plistPath)
	return nil
}

// ensureLogDir mkdirs the log dir the plist's StandardOut/ErrPath point at.
func (i *installer) ensureLogDir(p wardenPaths) error {
	if i.dryRun {
		i.logf("DRYRUN would: mkdir -p %s", p.logDir)
		return nil
	}
	if err := i.sys.mkdirAll(p.logDir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir %s: %w", p.logDir, err)
	}
	return nil
}

// bootout poll bounds — same shape as bin/ocserver's drop_and_load (25 x 0.2s = a
// ~5s bounded wait). A not-loaded label is confirmed gone on the FIRST probe, so an
// already-clean machine pays one `launchctl print` and zero sleeps.
const (
	bootoutPollAttempts = 25
	bootoutPollInterval = 200 * time.Millisecond
)

// bootoutUntilGone removes any existing registration UNDER THE EXACT label: it runs
// `launchctl bootout <target>` (a non-zero exit = "not currently loaded" and is
// tolerated — idempotent), then POLLS `launchctl print <target>` until launchd
// reports the label truly gone. The poll exists because bootout is ASYNC: the call
// returns while the label can linger registered, and a bootstrap issued in that
// window fails ("Bootstrap failed: 5: Input/output error" / "service already
// bootstrapped"), which is exactly how a re-install used to blow up. Returns true
// once the label is confirmed gone, false if it still lingers after the bounded
// wait (the caller decides — install warns and bootstraps anyway, byte-parity with
// bin/ocserver's drop_and_load). EXACT label only — NEVER pkill, NEVER a pattern.
func bootoutUntilGone(sys sysOps, target string) bool {
	_, _ = sys.run("launchctl", "bootout", target)
	for k := 0; k < bootoutPollAttempts; k++ {
		// `launchctl print` exits non-zero for an unregistered label → gone.
		if _, err := sys.run("launchctl", "print", target); err != nil {
			return true
		}
		sys.sleep(bootoutPollInterval)
	}
	return false
}

// launchctlReinstall boots out the existing instance UNDER THIS LABEL ONLY
// (idempotent reinstall; bootout error is tolerated = "not currently loaded"),
// POLLS until the old registration is truly gone (bootout is async — see
// bootoutUntilGone), then bootstraps fresh and kickstarts (gui-domain RunAtLoad
// does not reliably fire the initial run; kickstart -k forces it deterministically
// and is idempotent). EXACT label only — NEVER pkill, NEVER a pattern, NEVER the
// python daemons (bash step 5).
func (i *installer) launchctlReinstall(p wardenPaths) error {
	label := p.labelOrDefault()
	target := p.guiDomain + "/" + label
	if i.dryRun {
		i.logf("DRYRUN would run: launchctl bootout %s  (tolerate not-loaded)", target)
		i.logf("DRYRUN would: poll `launchctl print %s` until the label is gone (bootout is async; bounded ~%ds)", target, bootoutPollAttempts*int(bootoutPollInterval/time.Millisecond)/1000)
		i.logf("DRYRUN would run: launchctl bootstrap %s %s", p.guiDomain, p.plistPath)
		i.logf("DRYRUN would run: launchctl kickstart -k %s", target)
		return nil
	}
	// bootout + poll-until-gone: tolerate not-loaded; wait out launchd's async
	// deregistration so the bootstrap below never races the dying registration.
	if !bootoutUntilGone(i.sys, target) {
		i.logf("WARN: %s still registered ~%ds after bootout; bootstrapping anyway", label, bootoutPollAttempts*int(bootoutPollInterval/time.Millisecond)/1000)
	}
	if _, err := i.sys.run("launchctl", "bootstrap", p.guiDomain, p.plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap failed for %s: %w", p.plistPath, err)
	}
	if _, err := i.sys.run("launchctl", "kickstart", "-k", target); err != nil {
		return fmt.Errorf("launchctl kickstart failed for %s: %w", target, err)
	}
	i.logf("bootstrapped + kickstarted %s (exact label; python warden untouched)", label)
	return nil
}

// verify proves the job came up AND STAYS up (bash step 6): phase 1 waits up to 30s
// for the job to acquire a pid; phase 2 requires the SAME pid to hold across a ~6s
// settle window (a bad-token / unreachable-server warden is respawned by KeepAlive
// under a DIFFERENT pid, so "saw a pid once" is not proof of health). The 1s waits
// go through the injectable sleep seam so tests drive it instantly.
func (i *installer) verify(p wardenPaths) error {
	if i.dryRun {
		i.logf("DRYRUN: skipping live verification (no machine state changed)")
		return nil
	}
	label := p.labelOrDefault()
	target := p.guiDomain + "/" + label
	i.logf("verifying %s is alive AND STABLE...", label)

	// Phase 1: acquire a pid at all (up to 30 x 1s).
	var pid string
	for k := 0; k < 30; k++ {
		if pid = i.wardenPID(target); pid != "" {
			break
		}
		i.sys.sleep(time.Second)
	}
	if pid == "" {
		return fmt.Errorf("%s did not report a live PID within 30s", label)
	}
	// Phase 2: stability — the same pid must persist across ~6s.
	for k := 0; k < 6; k++ {
		i.sys.sleep(time.Second)
		now := i.wardenPID(target)
		if now != pid {
			return fmt.Errorf("%s is CRASH-LOOPING — pid did not hold across the settle window (first=%s, then=%s); likely bad token / server unreachable / auth reject", label, pid, orNone(now))
		}
	}
	i.logf("SUCCESS: %s is running and STABLE (pid=%s held >=6s). Logs: %s/ocwarden.{out,err}.log", label, pid, p.logDir)
	return nil
}

// wardenPID returns the job's live pid via `launchctl print`, or "" (a not-loaded
// label exits non-zero → treated as no pid).
func (i *installer) wardenPID(target string) string {
	out, err := i.sys.run("launchctl", "print", target)
	if err != nil {
		return ""
	}
	if m := launchctlPIDRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}

// runInstall executes the six steps in order, failing fast (bash `set -e`).
func (i *installer) runInstall(p wardenPaths) error {
	idDisplay := p.ocID
	if idDisplay == "" {
		idDisplay = "<derive-from-token-sub>"
	}
	i.logf("resolved: ROOT=%s HOME=%s OC_BASE=%s OC_ID=%s", p.root, p.home, p.ocBase, idDisplay)
	i.logf("targets:  TOKFILE=%s PLIST=%s BIN=%s SRC=%s", p.tokfile, p.plistPath, p.binPath, p.srcExe)
	if p.ocAgentSrc != "" {
		i.logf("ocagent:  LOCAL OVERRIDE (OC_AGENT_BIN) SRC=%s -> BIN=%s (home sibling; runtime-discovered, not in plist)", p.ocAgentSrc, p.ocAgentBin)
	} else {
		i.logf("ocagent:  DOWNLOAD %s%s -> BIN=%s (home sibling; runtime-discovered, not in plist)", p.ocBase, agentBinaryPath, p.ocAgentBin)
	}
	// Resolve claude NOW, in the installer's env (see resolveClaude seam doc), and
	// carry the result into the plist render below. Resolution is read-only (a
	// LookPath + at most two `claude --version` probes), so it runs under DRYRUN
	// too and its outcome is part of the dry-run report. nil seam = no stamp
	// (test fixtures / legacy callers keep the historical render).
	if i.resolveClaude != nil {
		p.claudeBin, p.plistPATH = i.resolveClaude()
		switch {
		case p.claudeBin == "":
			i.logf("WARNING: claude CLI not found — the warden will refuse every spawn (claude_bin_unresolved).")
			i.logf("WARNING: fix: install claude (e.g. `npm install -g @anthropic-ai/claude-code`), or export OC_CLAUDE_BIN=/absolute/path/to/claude, then re-run `ocwarden install` (safe — idempotent).")
		case p.plistPATH != "":
			i.logf("claude:   %s (stamped OC_CLAUDE_BIN + installer PATH into the plist — shim needs it)", p.claudeBin)
		default:
			i.logf("claude:   %s (stamped OC_CLAUDE_BIN into the plist)", p.claudeBin)
		}
	}
	if i.dryRun {
		i.logf("DRY-RUN mode: no file writes / no launchctl / no verification.")
	}
	// Guard BEFORE any mutation: refuse if a warden for a different machine is already
	// installed here (unless --force). Runs and can refuse identically under DRYRUN.
	if err := i.guard(p); err != nil {
		return err
	}
	if err := i.copyBinary(p); err != nil {
		return err
	}
	if err := i.installOcAgent(p); err != nil {
		return err
	}
	if err := i.writeTokfile(p); err != nil {
		return err
	}
	if err := i.ensureLogDir(p); err != nil {
		return err
	}
	if err := i.writePlist(p); err != nil {
		return err
	}
	if err := i.launchctlReinstall(p); err != nil {
		return err
	}
	if err := i.verify(p); err != nil {
		return err
	}
	if i.dryRun {
		i.logf("DRYRUN complete — no machine state changed.")
	}
	return nil
}

// installCmd is the thin `ocwarden install` entry point: it resolves the running
// binary + uid (the only real-OS reads outside the seam), wires realSysOps(), and
// runs the six steps. Returns 0 on success, 1 on any failure.
func installCmd(env func(string) string, out io.Writer, force bool) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(out, "[ocwarden install] FATAL: cannot resolve own binary path: %v\n", err)
		return 1
	}
	// Resolve symlinks so a launcher-symlinked binary copies its real target, not the
	// dangling link. A resolve failure is non-fatal — fall back to the raw path.
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	// Wire the ocagent DOWNLOAD seam from the same OC_BASE/OC_TOKEN env the install
	// already resolves (Config). agentGet pulls GET /api/agent/binary (PUBLIC — the token
	// is sent as a bonus but not required, matching handle_agent_binary); agentProbe execs
	// the download `--help` and requires exit 0 (verify-before-swap). Unused when
	// OC_AGENT_BIN provides a local override or under DRYRUN.
	cfg := loadConfig(env)
	client := &http.Client{Timeout: selfUpdateHTTPTimeout}
	probeOps := osUpdaterOps{runner: execRunner{timeout: selfUpdateProbeBudget}}
	i := &installer{
		out:        out,
		dryRun:     env(dryRunEnv) == "1",
		force:      force,
		sys:        realSysOps(),
		agentGet:   httpGetter(client, cfg.Base, cfg.Token),
		agentProbe: probeOps.probe,
	}
	// Install-time claude resolution → OC_CLAUDE_BIN plist stamp. lookup reuses the
	// RUNTIME resolver (resolveClaudeBin: OC_CLAUDE_BIN env → LookPath → common
	// dirs) but evaluated in the INSTALLER's env, where the path is actually
	// discoverable; realClaudeProbe then decides whether the minimal launchd PATH
	// suffices or the installer PATH must ride along (shim/shebang).
	i.resolveClaude = func() (string, string) {
		return resolveClaudeForInstall(env, func() string { return resolveClaudeBin(env) }, realClaudeProbe, i.logf)
	}
	p, err := resolvePaths(env, exe, os.Getuid())
	if err != nil {
		i.errf("%v", err)
		return 1
	}
	if err := i.runInstall(p); err != nil {
		i.errf("%v (re-run is safe — idempotent)", err)
		return 1
	}
	return 0
}
