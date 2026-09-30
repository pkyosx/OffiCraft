// `ocwarden install` — the one-key warden installer (the sole installer).
//
// The ONLY process action is launchctl by the EXACT label — never pkill /
// pattern-kill / killall. The binary is the committed prebuilt bin/ocwarden;
// install does NOT rebuild (a fresh machine with no Go toolchain must still be
// able to install).
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
	// Imported by production code on purpose: testing.Testing() is how the real
	// seam constructors know they are about to hand the LIVE machine to `go test`.
	"testing"
	"time"
)

// Byte-identical to the committed plist template's <Label>.
const wardenLabel = "com.officraft.ocwarden"

const dryRunEnv = "WARDEN_INSTALL_DRYRUN"

// The only guard before OC_BASE is interpolated into the plist XML.
var ocBaseShape = regexp.MustCompile(`^https?://[^\s"'<>&]+$`)

var launchctlPIDRe = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)`)

type sysOps struct {
	run       func(name string, args ...string) (string, error)
	mkdirAll  func(path string, perm os.FileMode) error
	writeFile func(path string, data []byte, perm os.FileMode) error
	readFile  func(path string) ([]byte, error)
	rename    func(oldpath, newpath string) error
	remove    func(path string) error
	chmod     func(path string, mode os.FileMode) error
	statMode  func(path string) (os.FileMode, error)
	sleep     func(time.Duration)
}

// refuseInTestBinary is the ONLY live tripwire on the functions that wire the
// real machine in: no source scan remains in this tree. It has fired for real —
// a test reached teardownCmd, which built its own effects, and booted out the
// developer machine's live com.officraft.ocwarden job.
//
// os.Exit, not panic: sseTransport.handlePayload (transport.go) recovers every
// dispatched CommandDeps closure, CommandDeps.Teardown included, so a panic
// would be downgraded to one log line.
func refuseInTestBinary(fn string) {
	if !testing.Testing() {
		return
	}
	fmt.Fprintf(os.Stderr, "\nFATAL: %s was reached from a test binary.\n"+
		"The real host seam must NEVER be constructed under `go test` — doing so wires the\n"+
		"test process to the LIVE launchd gui domain, where an install/teardown would boot\n"+
		"out this machine's real com.officraft.ocwarden job.\n"+
		"Every entry point must take its effects from an injected seam, never by building\n"+
		"the real one itself. See hostSeam in install.go.\n", fn)
	os.Exit(1)
}

func realSysOps() sysOps {
	refuseInTestBinary("realSysOps")
	r := newCmdRunner(30 * time.Second)
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

// hostSeam is the SINGLE construction point for every real-host effect the
// install / teardown / uninstall-RPC entry points can reach. Nothing rebinds
// newHostSeam in this tree, so a test that calls an entry point gets the real
// seam and dies in refuseInTestBinary. Wiring assembled inline (a `sysOps{…}`
// literal) is caught only by main.go's execRunner.exec opening with
// refuseInTestBinary — at the syscall, not before the tests run.
type hostSeam struct {
	sys          sysOps
	versionProbe func(bin, pathEnv, home string) error
	agentGet     func(base, token string) getter
	agentProbe   func(bin string) error
}

func realHostSeam() hostSeam {
	refuseInTestBinary("realHostSeam")
	probeOps := osUpdaterOps{runner: newCmdRunner(selfUpdateProbeBudget)}
	return hostSeam{
		sys:          realSysOps(),
		versionProbe: realVersionProbe,
		agentGet: func(base, token string) getter {
			return httpGetter(&http.Client{Timeout: selfUpdateRequestBudget}, base, token)
		},
		agentProbe: probeOps.probe,
	}
}

// NEVER call realHostSeam directly from an entry point — that bypasses the one
// name this indirection gives a test.
var newHostSeam = realHostSeam

type installer struct {
	out    io.Writer
	dryRun bool
	force  bool
	sys    sysOps

	tag string

	resolveClaude func() (claudeBin, plistPATH string)
	resolveCodex  func() (codexBin, plistPATH string)

	agentGet   getter
	agentProbe func(bin string) error
}

func (i *installer) subcmdTag() string {
	if i.tag == "" {
		return "install"
	}
	return i.tag
}
func (i *installer) logf(format string, a ...any) {
	fmt.Fprintf(i.out, "[ocwarden "+i.subcmdTag()+"] "+format+"\n", a...)
}
func (i *installer) errf(format string, a ...any) {
	fmt.Fprintf(i.out, "[ocwarden "+i.subcmdTag()+"] FATAL: "+format+"\n", a...)
}

type wardenPaths struct {
	root       string
	home       string
	namespace  string
	label      string
	srcExe     string
	ocBase     string
	ocToken    string
	ocID       string
	tokfile    string
	laDir      string
	plistPath  string
	logDir     string
	binPath    string
	anchorSrc  string
	anchorPath string
	guiDomain  string
	ocAgentSrc string
	// Must be relayed: a launchd job's env is EXACTLY its plist, so an operator
	// exporting OC_CLAUDE_CRED_CHECK in a shell changes nothing, and the escape
	// hatch the spawn-time refusal advertises could not be pressed.
	credCheck string
	// Not stamped into the plist: the runtime warden finds ocagent next to its
	// own executable (resolveOcAgentBin).
	ocAgentBin string
	claudeBin  string
	codexBin   string
	plistPATH  string
}

func (p wardenPaths) labelOrDefault() string {
	if p.label != "" {
		return p.label
	}
	return wardenLabel
}

// The env contract (OC_BASE / OC_TOKEN / OC_ID) is defined to align with the
// server's boot_command.
func resolvePaths(env func(string) string, exe string, uid int) (wardenPaths, error) {
	home := env("HOME")
	if home == "" {
		return wardenPaths{}, errors.New("HOME must be set")
	}
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return wardenPaths{}, err
	}
	label := wardenLabelFor(ns)
	root := officraftRootFor(home, ns)

	// OC_BASE gets baked into the plist, so an unset value is refused rather
	// than defaulted. The check is on the ENV, not the value: a station host
	// legitimately installs with OC_BASE pointing at loopback (defaultBase).
	ocBase, ocBaseConfigured := baseFromEnv(env)
	if !ocBaseConfigured {
		return wardenPaths{}, errors.New("OC_BASE is not set — refusing to install a warden that does not know which station to talk to. " +
			"Re-run with OC_BASE set to the station URL (e.g. OC_BASE=https://station.example ocwarden install). " +
			"Guessing " + defaultBase + " here would be written into this machine's launchd job permanently, and members started from it would join whatever is listening there")
	}
	ocBase = strings.TrimRight(ocBase, "/") // mirror loadConfig
	if !ocBaseShape.MatchString(ocBase) {
		return wardenPaths{}, fmt.Errorf("OC_BASE must be http(s)://host[:port] with no whitespace/XML-special chars, got: %s", ocBase)
	}

	ocToken := env("OC_TOKEN")
	if ocToken == "" {
		return wardenPaths{}, errors.New("OC_TOKEN is required (the exec-warden member token; NOT the telemetry warden's). Usage: OC_BASE=<base> OC_TOKEN=<jwt> [OC_ID=<id>] ocwarden install")
	}

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
		tokfile:    tokfileFor(home, ns),
		laDir:      filepath.Join(home, "Library", "LaunchAgents"),
		plistPath:  filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		logDir:     filepath.Join(root, "warden", "log"),
		binPath:    filepath.Join(root, "warden", "ocwarden"),
		anchorSrc:  filepath.Join(filepath.Dir(exe), "officraft"),
		anchorPath: filepath.Join(root, "warden", "officraft"),
		guiDomain:  fmt.Sprintf("gui/%d", uid),
		ocAgentSrc: ocAgentSrc,
		ocAgentBin: filepath.Join(root, "warden", "ocagent"),
		credCheck:  relayedCredCheck(env),
	}, nil
}

const wardenPlistPATH = "/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// ProgramArguments names the TCC identity anchor (which forks the sibling
// ocwarden), not ocwarden: launchd's job leader is the responsible process for
// the whole tree, and ocwarden is replaced by self-update, which would void the
// machine's privacy grants on every update.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- RENDERED by ocwarden install for ROOT=%[10]s — do not edit by hand; re-run the installer. -->
<plist version="1.0">
<dict>
    <key>Label</key><string>%[7]s</string>
    <key>ProgramArguments</key>
    <array><string>%[2]s</string></array>
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

func renderPlist(p wardenPaths) string {
	extraEnv := ""
	if p.namespace != "" {
		extraEnv += "\n        <key>OC_NAMESPACE</key><string>" + p.namespace + "</string>"
	}
	if p.claudeBin != "" {
		extraEnv += "\n        <key>OC_CLAUDE_BIN</key><string>" + xmlEscape(p.claudeBin) + "</string>"
	}
	if p.codexBin != "" {
		extraEnv += "\n        <key>OC_CODEX_BIN</key><string>" + xmlEscape(p.codexBin) + "</string>"
	}
	if p.credCheck != "" {
		extraEnv += "\n        <key>OC_CLAUDE_CRED_CHECK</key><string>" + xmlEscape(p.credCheck) + "</string>"
	}
	pathVal := p.plistPATH
	if pathVal == "" {
		pathVal = wardenPlistPATH
	}
	return fmt.Sprintf(plistTemplate, p.root, p.anchorPath, p.ocBase, p.home, p.tokfile, p.logDir, p.labelOrDefault(), extraEnv, xmlEscape(pathVal), xmlCommentSafe(p.root))
}

func relayedCredCheck(env func(string) string) string {
	if strings.TrimSpace(env("OC_CLAUDE_CRED_CHECK")) == "0" {
		return "0"
	}
	return ""
}

func xmlCommentSafe(s string) string {
	return strings.ReplaceAll(s, "--", "- -")
}

func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	).Replace(s)
}

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

func stampableClaude(p string) bool {
	return filepath.IsAbs(p) && !strings.ContainsAny(p, " \t\n\r\"'<>&")
}

// Why install time: the runtime resolveClaudeBin fallbacks (LookPath under the
// minimal launchd PATH, common dirs) miss a version-manager claude (asdf/nvm/
// volta), giving claude_bin_unresolved on every spawn; `ocwarden install` runs
// where the path is discoverable (the operator's shell, or a serve process whose
// plist carries OC_CLAUDE_BIN from `bin/ocserver install`). The probe catches
// the shim/shebang trap: an asdf shim or `#!/usr/bin/env node` launcher needs
// its manager/interpreter on PATH, so the installer's PATH then rides along.
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
		return cand, ""
	}
	if userPATH := env("PATH"); userPATH != "" && probe(cand, userPATH, home) == nil {
		logf("claude at %s needs the installer's PATH to run (version-manager shim / env-shebang) — stamping the full installer PATH into the warden plist alongside OC_CLAUDE_BIN", cand)
		return cand, userPATH
	}
	logf("WARN: claude at %s failed `--version` under both the minimal launchd PATH and the installer PATH — stamping OC_CLAUDE_BIN best-effort; spawns may still fail (check that claude runs headless)", cand)
	return cand, ""
}

// A cold Node CLI can take seconds; a wedged shim must not hang the install.
const claudeProbeBudget = 20 * time.Second

func realVersionProbe(bin, pathEnv, home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), claudeProbeBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = []string{"PATH=" + pathEnv, "HOME=" + home}
	return cmd.Run()
}

// Match token sub to token sub, not only OC_ID: a half-install leaves a tokfile
// whose sub equals any re-minted token's, and a stray OC_ID must not make the
// machine re-installing itself look foreign.
func (i *installer) guard(p wardenPaths) error {
	if i.force {
		i.logf("--force: skipping one-warden-per-machine guard")
		return nil
	}
	raw, err := i.sys.readFile(p.tokfile)
	if err != nil {
		return nil
	}
	existing := jwtSub(strings.TrimSpace(string(raw)))
	if existing == "" {
		return nil
	}
	if newSub := jwtSub(p.ocToken); existing == newSub || (p.ocID != "" && existing == p.ocID) {
		i.logf("guard: existing warden is the same machine (%s) — idempotent re-provision", existing)
		return nil
	}
	// Bare `ocwarden teardown` fails closed (no implicit canonical target),
	// and `--canonical` from a namespaced install would hit the LIVE canonical
	// warden — so spell the command for THIS instance.
	teardownHint := "ocwarden teardown --canonical"
	if p.namespace != "" {
		teardownHint = "OC_NAMESPACE=" + p.namespace + " ocwarden teardown"
	}
	return fmt.Errorf("refusing: a warden for machine %s is already installed on this box; run '%s' first, or pass --force to replace", existing, teardownHint)
}

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
	if err := i.sys.chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod temp binary %s: %w", tmp, err)
	}
	if err := i.sys.rename(tmp, p.binPath); err != nil {
		return fmt.Errorf("atomic rename binary -> %s: %w", p.binPath, err)
	}
	i.logf("installed binary (0755): %s", p.binPath)
	return nil
}

// Replacing even identical bytes changes the inode and can invalidate TCC's
// grant, so an existing anchor is always preserved.
func (i *installer) copyAnchorIfAbsent(p wardenPaths) error {
	if _, err := i.sys.readFile(p.anchorPath); err == nil {
		i.logf("fixed identity anchor already exists; preserving %s", p.anchorPath)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check fixed identity anchor %s: %w", p.anchorPath, err)
	}
	if i.dryRun {
		i.logf("DRYRUN would: copy fixed identity anchor %s -> %s only if absent", p.anchorSrc, p.anchorPath)
		return nil
	}
	if err := i.sys.mkdirAll(filepath.Dir(p.anchorPath), 0o755); err != nil {
		return fmt.Errorf("mkdir anchor dir %s: %w", filepath.Dir(p.anchorPath), err)
	}
	data, err := i.sys.readFile(p.anchorSrc)
	if err == nil && len(data) == 0 {
		// A zero-byte sibling is not an anchor, and the never-replace rule would
		// preserve it forever.
		err = fmt.Errorf("anchor source %s is empty", p.anchorSrc)
	}
	if err != nil {
		// The cockpit's one-liner downloads ocwarden ALONE, so on remote onboarding
		// the embedded copy is the only anchor; the bytes must be identical either
		// way (anchor_embed.go).
		if embedded := embeddedAnchor(); len(embedded) > 0 {
			i.logf("no anchor beside ocwarden; using the copy embedded in this binary")
			data = embedded
		} else {
			return fmt.Errorf("read fixed identity anchor %s (and this ocwarden carries no embedded anchor): %w", p.anchorSrc, err)
		}
	}
	if err := i.sys.writeFile(p.anchorPath, data, 0o755); err != nil {
		return fmt.Errorf("write fixed identity anchor %s: %w", p.anchorPath, err)
	}
	if err := i.sys.chmod(p.anchorPath, 0o755); err != nil {
		return fmt.Errorf("chmod fixed identity anchor %s: %w", p.anchorPath, err)
	}
	i.logf("installed fixed identity anchor (0755): %s", p.anchorPath)
	return nil
}

func (i *installer) installOcAgent(p wardenPaths) error {
	if p.ocAgentSrc == p.ocAgentBin && p.ocAgentSrc != "" {
		i.logf("ocagent source is already the installed home binary (%s); skipping self-copy", p.ocAgentBin)
		return nil
	}
	dir := filepath.Dir(p.ocAgentBin)

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
	if err := i.sys.chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod temp ocagent %s: %w", tmp, err)
	}
	// Verify-before-swap, download path only: a corrupt / wrong-arch ocagent at
	// the sibling path would make every spawned agent exit 127. The local
	// override is a dev-controlled file, possibly cross-arch, so it is not probed.
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

// Shared by `ocwarden install` and the self-renewal loop: one implementation
// of the credential write, so a fix cannot land in only one copy.
type tokfileWriter struct {
	mkdirAll  func(path string, perm os.FileMode) error
	writeFile func(path string, data []byte, perm os.FileMode) error
	chmod     func(path string, mode os.FileMode) error
	rename    func(oldpath, newpath string) error
	statMode  func(path string) (os.FileMode, error)
	remove    func(path string) error
}

func (s sysOps) tokfileWriter() tokfileWriter {
	return tokfileWriter{
		mkdirAll:  s.mkdirAll,
		writeFile: s.writeFile,
		chmod:     s.chmod,
		rename:    s.rename,
		statMode:  s.statMode,
		remove:    s.remove,
	}
}

func osTokfileWriter() tokfileWriter {
	return tokfileWriter{
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		chmod:     os.Chmod,
		rename:    os.Rename,
		statMode: func(p string) (os.FileMode, error) {
			fi, err := os.Stat(p)
			if err != nil {
				return 0, err
			}
			return fi.Mode(), nil
		},
		remove: os.Remove,
	}
}

// A failure at ANY step leaves the destination untouched, which is what lets
// the renewal caller treat "write failed" as "keep the old credential".
func (w tokfileWriter) write(path, token string) error {
	dir := filepath.Dir(path)
	if err := w.mkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir tokfile dir %s: %w", dir, err)
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".exec-warden.tok.%d", os.Getpid()))
	if err := w.writeFile(tmp, []byte(token), 0o600); err != nil {
		w.cleanup(tmp)
		return fmt.Errorf("write temp tokfile %s: %w", tmp, err)
	}
	// os.WriteFile's mode is masked by umask; re-assert 0600.
	if err := w.chmod(tmp, 0o600); err != nil {
		w.cleanup(tmp)
		return fmt.Errorf("chmod temp tokfile %s: %w", tmp, err)
	}
	// Verify perms on the TEMP, before the rename, so rename is the LAST step
	// that can fail. Verifying the destination afterwards once made the renewal
	// loop report "previous credential untouched" about a file it had just
	// overwritten, and renew again forever.
	mode, err := w.statMode(tmp)
	if err != nil {
		w.cleanup(tmp)
		return fmt.Errorf("stat temp tokfile %s: %w", tmp, err)
	}
	if mode.Perm() != 0o600 {
		w.cleanup(tmp)
		return fmt.Errorf("temp tokfile perms are not 0600: %s (got %o)", tmp, mode.Perm())
	}
	if err := w.rename(tmp, path); err != nil {
		w.cleanup(tmp)
		return fmt.Errorf("atomic rename tokfile -> %s: %w", path, err)
	}
	return nil
}

// Every failure path removes the temp: it holds (part of) a live credential.
func (w tokfileWriter) cleanup(tmp string) {
	if w.remove != nil {
		_ = w.remove(tmp)
	}
}

func (i *installer) writeTokfile(p wardenPaths) error {
	if i.dryRun {
		i.logf("DRYRUN would: mkdir -p %s; write <token> to a 0600 temp then atomic rename -> %s",
			filepath.Dir(p.tokfile), p.tokfile)
		return nil
	}
	if err := i.sys.tokfileWriter().write(p.tokfile, p.ocToken); err != nil {
		return err
	}
	i.logf("wrote tokfile (0600): %s", p.tokfile)
	return nil
}

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

// Same bounds as bin/ocserver's drop_and_load (25 x 0.2s); keep the two in sync.
const (
	bootoutPollAttempts = 25
	bootoutPollInterval = 200 * time.Millisecond
)

// bootout is ASYNC: a bootstrap issued while the label still lingers fails
// ("Bootstrap failed: 5: Input/output error"), which is how re-installs used to
// break — hence the poll.
func bootoutUntilGone(sys sysOps, target string) bool {
	_, _ = sys.run("launchctl", "bootout", target)
	for k := 0; k < bootoutPollAttempts; k++ {
		if _, err := sys.run("launchctl", "print", target); err != nil {
			return true
		}
		sys.sleep(bootoutPollInterval)
	}
	return false
}

const (
	registerPollAttempts = bootoutPollAttempts
	registerPollInterval = bootoutPollInterval
)

func registeredUntilFound(sys sysOps, target string) error {
	var err error
	for k := 0; k < registerPollAttempts; k++ {
		if _, err = sys.run("launchctl", "print", target); err == nil {
			return nil
		}
		sys.sleep(registerPollInterval)
	}
	return err
}

// kickstart -k because gui-domain RunAtLoad does not reliably fire the initial
// run.
func (i *installer) launchctlReinstall(p wardenPaths) error {
	label := p.labelOrDefault()
	target := p.guiDomain + "/" + label
	if i.dryRun {
		i.logf("DRYRUN would run: launchctl bootout %s  (tolerate not-loaded)", target)
		i.logf("DRYRUN would: poll `launchctl print %s` until the label is gone (bootout is async; bounded ~%ds)", target, bootoutPollAttempts*int(bootoutPollInterval/time.Millisecond)/1000)
		i.logf("DRYRUN would run: launchctl bootstrap %s %s", p.guiDomain, p.plistPath)
		i.logf("DRYRUN would: poll `launchctl print %s` until the label is registered (bootstrap can exit 0 and register nothing; bounded ~%ds, non-fatal)", target, registerPollAttempts*int(registerPollInterval/time.Millisecond)/1000)
		i.logf("DRYRUN would run: launchctl kickstart -k %s", target)
		return nil
	}
	if !bootoutUntilGone(i.sys, target) {
		i.logf("WARN: %s still registered ~%ds after bootout; bootstrapping anyway", label, bootoutPollAttempts*int(bootoutPollInterval/time.Millisecond)/1000)
	}
	if _, err := i.sys.run("launchctl", "bootstrap", p.guiDomain, p.plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap failed for %s: %w", p.plistPath, err)
	}
	// `launchctl bootstrap` can exit 0 and register NOTHING (kickstart then fails
	// with a misleading exit 113), but registration can also merely lag, so a
	// timeout here is not fatal; only when kickstart ALSO fails is the label
	// really absent.
	unregistered := registeredUntilFound(i.sys, target)
	if unregistered != nil {
		i.logf("WARN: launchd still does not know %s ~%ds after bootstrap exited 0; kickstarting anyway", label, registerPollAttempts*int(registerPollInterval/time.Millisecond)/1000)
	}
	if _, err := i.sys.run("launchctl", "kickstart", "-k", target); err != nil {
		if unregistered != nil {
			// Blame bootstrap but keep kickstart's error too: dropping it would hide
			// a second, unrelated failure reason.
			return fmt.Errorf("launchctl bootstrap exited 0 but registered nothing — launchd does not know %s, so the job was never loaded; the plist to look at is %s: %w (the next verb then reported: %v)", target, p.plistPath, unregistered, err)
		}
		return fmt.Errorf("launchctl kickstart failed for %s: %w", target, err)
	}
	i.logf("bootstrapped + kickstarted %s (exact label; python warden untouched)", label)
	return nil
}

// A bad-token / unreachable-server warden is respawned by KeepAlive under a
// DIFFERENT pid, so the same pid must hold across the settle window.
func (i *installer) verify(p wardenPaths) error {
	if i.dryRun {
		i.logf("DRYRUN: skipping live verification (no machine state changed)")
		return nil
	}
	label := p.labelOrDefault()
	target := p.guiDomain + "/" + label
	i.logf("verifying %s is alive AND STABLE...", label)

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
	if i.resolveClaude != nil {
		p.claudeBin, p.plistPATH = i.resolveClaude()
		if p.claudeBin != "" && p.plistPATH != "" {
			i.logf("claude:   %s (stamped OC_CLAUDE_BIN + installer PATH into the plist — shim needs it)", p.claudeBin)
		} else if p.claudeBin != "" {
			i.logf("claude:   %s (stamped OC_CLAUDE_BIN into the plist)", p.claudeBin)
		}
	}
	if i.resolveCodex != nil {
		var codexPATH string
		p.codexBin, codexPATH = i.resolveCodex()
		if codexPATH != "" {
			p.plistPATH = codexPATH
		}
		if p.codexBin != "" {
			i.logf("codex:    %s (stamped OC_CODEX_BIN into the plist)", p.codexBin)
		}
	}
	if i.resolveClaude != nil && p.claudeBin == "" &&
		(i.resolveCodex == nil || p.codexBin == "") {
		i.errf("neither Claude Code nor Codex CLI is available — NOTHING was installed")
		i.errf("runtime_bin_unresolved: install a provider or set OC_CLAUDE_BIN/OC_CODEX_BIN, then re-run safely")
		return errors.New("runtime_bin_unresolved: install claude or codex, or set OC_CLAUDE_BIN/OC_CODEX_BIN")
	}
	if i.dryRun {
		i.logf("DRY-RUN mode: no file writes / no launchctl / no verification.")
	}
	if err := i.guard(p); err != nil {
		return err
	}
	if err := i.copyBinary(p); err != nil {
		return err
	}
	if err := i.copyAnchorIfAbsent(p); err != nil {
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

func installCmd(env func(string) string, out io.Writer, force bool) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(out, "[ocwarden install] FATAL: cannot resolve own binary path: %v\n", err)
		return 1
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	cfg := loadConfig(env)
	host := newHostSeam()
	i := &installer{
		out:        out,
		dryRun:     env(dryRunEnv) == "1",
		force:      force,
		sys:        host.sys,
		agentGet:   host.agentGet(cfg.Base, cfg.Token),
		agentProbe: host.agentProbe,
	}
	i.resolveClaude = func() (string, string) {
		return resolveClaudeForInstall(env, func() string { return resolveClaudeBin(env) }, host.versionProbe, i.logf)
	}
	i.resolveCodex = func() (string, string) {
		candidate := resolveCodexBin(env)
		if candidate == "" || !stampableClaude(candidate) {
			return "", ""
		}
		if host.versionProbe(candidate, wardenPlistPATH, env("HOME")) == nil {
			return candidate, ""
		}
		if path := env("PATH"); path != "" &&
			host.versionProbe(candidate, path, env("HOME")) == nil {
			return candidate, path
		}
		return candidate, ""
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
