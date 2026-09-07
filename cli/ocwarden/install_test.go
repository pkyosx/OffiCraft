// Skeleton generated from cli/ocwarden/install.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestRefuseInTestBinary(t *testing.T) {
	t.Skip("TODO: refuseInTestBinary is the LAST-RESORT runtime tripwire on the two functions that wire the real machine in.")
}

func TestRealSysOps(t *testing.T) {
	t.Skip("TODO: realSysOps wires the seam to the real OS.")
}

func TestRealHostSeam(t *testing.T) {
	t.Skip("TODO: realHostSeam is the ONLY place the real OS is wired into install/teardown.")
}

func TestSubcmdTag(t *testing.T) {
	t.Skip("TODO: subcmdTag is the log prefix for whichever subcommand owns this installer.")
}

func TestErrf(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestLabelOrDefault(t *testing.T) {
	t.Skip("TODO: labelOrDefault is the launchd label every install/teardown/launchctl step acts on: the namespace-derived p.label when resolved, else the canonical wardenLabel (empty namespace / zero-value fixtures — byte-identical either way).")
}

func TestResolvePaths(t *testing.T) {
	t.Skip("TODO: resolvePaths reads the env contract (OC_BASE / OC_TOKEN / OC_ID — DEFINED to align with the server's boot_command) and derives every per-machine path.")
}

func TestRenderPlist(t *testing.T) {
	t.Skip("TODO: renderPlist substitutes the real per-machine paths into the template.")
}

func TestRelayedCredCheck(t *testing.T) {
	t.Skip("TODO: relayedCredCheck returns the OC_CLAUDE_CRED_CHECK value to stamp into the warden plist: only the explicit opt-out \"0\" is relayed, everything else (unset, \"1\", junk) renders nothing and leaves the gate on.")
}

func TestXmlEscape(t *testing.T) {
	t.Skip("TODO: xmlEscape escapes the five XML-special characters for a plist <string> value.")
}

func TestXmlWellFormed(t *testing.T) {
	t.Skip("TODO: xmlWellFormed asserts the rendered plist parses as XML (the machine-independent equivalent of the bash installer's `plutil -lint`: a malformed render fails here in BOTH dry-run and live, so pre-land verification actually catches it).")
}

func TestResolveClaudeForInstall(t *testing.T) {
	t.Skip("TODO: resolveClaudeForInstall resolves the claude CLI in the INSTALLER's environment and decides what to stamp into the warden plist.")
}

func TestRealClaudeProbe(t *testing.T) {
	t.Skip("TODO: realClaudeProbe execs `<bin> --version` under EXACTLY the given PATH+HOME (the same env shape the launchd plist grants), answering \"would this claude run under the warden's runtime env\".")
}

func TestGuard(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- the six steps --------------------------------------------------------------------------- guard enforces one warden per machine.")
}

func TestCopyBinary(t *testing.T) {
	t.Skip("TODO: copyBinary makes the install self-contained: it copies the running binary (p.srcExe, symlinks already resolved by installCmd) to the STABLE home target p.binPath ($HOME/.officraft/warden/ocwarden) mode 0755 via a fresh temp + atomic rename, so the durable warden runs from home and survives deletion of the temp/clone copy it was launched from.")
}

func TestCopyAnchorIfAbsent(t *testing.T) {
	t.Skip("TODO: copyAnchorIfAbsent installs the fixed TCC identity launcher exactly once.")
}

func TestInstallOcAgent(t *testing.T) {
	t.Skip("TODO: installOcAgent completes the SELF-CONTAINED install by putting a working ocagent at the stable home sibling p.ocAgentBin ($HOME/.officraft/warden/ocagent) mode 0755, so the spawn shim execs a home-owned binary that survives deletion of the run-clone it came from.")
}

func TestDownloadOcAgent(t *testing.T) {
	t.Skip("TODO: downloadOcAgent GETs the committed prebuilt ocagent from the server (GET /api/agent/binary, PUBLIC) via i.agentGet and returns its bytes.")
}

func TestTokfileWriter(t *testing.T) {
	t.Skip("TODO: tokfileWriter projects the install seam onto the narrow write.")
}

func TestOsTokfileWriter(t *testing.T) {
	t.Skip("TODO: osTokfileWriter wires the narrow write to the real filesystem for callers that have no sysOps of their own (the self-renewal loop).")
}

func TestCleanup(t *testing.T) {
	t.Skip("TODO: cleanup removes a temp that will never be renamed into place.")
}

func TestWriteTokfile(t *testing.T) {
	t.Skip("TODO: writeTokfile writes the exec-warden token 0600.")
}

func TestWritePlist(t *testing.T) {
	t.Skip("TODO: writePlist renders the plist, asserts it is well-formed XML, then (live) mkdirs LaunchAgents, writes the file, and runs `plutil -lint` for parity (bash step 4).")
}

func TestEnsureLogDir(t *testing.T) {
	t.Skip("TODO: ensureLogDir mkdirs the log dir the plist's StandardOut/ErrPath point at.")
}

func TestBootoutUntilGone(t *testing.T) {
	t.Skip("TODO: bootoutUntilGone removes any existing registration UNDER THE EXACT label: it runs `launchctl bootout <target>` (a non-zero exit = \"not currently loaded\" and is tolerated — idempotent), then POLLS `launchctl print <target>` until launchd reports the label truly gone.")
}

func TestRegisteredUntilFound(t *testing.T) {
	t.Skip("TODO: registeredUntilFound POLLS `launchctl print <target>` until launchd reports the label registered.")
}

func TestLaunchctlReinstall(t *testing.T) {
	t.Skip("TODO: launchctlReinstall boots out the existing instance UNDER THIS LABEL ONLY (idempotent reinstall; bootout error is tolerated = \"not currently loaded\"), POLLS until the old registration is truly gone (bootout is async — see bootoutUntilGone), then bootstraps fresh and kickstarts (gui-domain RunAtLoad does not reliably fire the initial run; kickstart -k forces it deterministically and is idempotent).")
}

func TestVerify(t *testing.T) {
	t.Skip("TODO: verify proves the job came up AND STAYS up (bash step 6): phase 1 waits up to 30s for the job to acquire a pid; phase 2 requires the SAME pid to hold across a ~6s settle window (a bad-token / unreachable-server warden is respawned by KeepAlive under a DIFFERENT pid, so \"saw a pid once\" is not proof of health).")
}

func TestWardenPID(t *testing.T) {
	t.Skip("TODO: wardenPID returns the job's live pid via `launchctl print`, or \"\" (a not-loaded label exits non-zero → treated as no pid).")
}

func TestOrNone(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestRunInstall(t *testing.T) {
	t.Skip("TODO: runInstall executes the six steps in order, failing fast (bash `set -e`).")
}

func TestInstallCmd(t *testing.T) {
	t.Skip("TODO: installCmd is the thin `ocwarden install` entry point: it resolves the running binary + uid (with resolveClaudeBin/resolveCodexBin's read-only LookPath+stat, the only real-OS reads outside the seam), takes its effects from newHostSeam(), and runs the six steps.")
}
