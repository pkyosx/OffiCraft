package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// jwtWardenTwo is a second three-part credential whose payload decodes to
// {"sub":"warden-2"} — a DIFFERENT machine than jwtWardenOne.
const jwtWardenTwo = "header.eyJzdWIiOiJ3YXJkZW4tMiJ9.signature"

// sysRecorder is the sysOps double. Every side effect appends one line to calls
// (in order), reads are answered from files, and any single step is made to fail
// by keying fail with the same line the call records.
type sysRecorder struct {
	calls []string
	files map[string]string
	perms map[string]os.FileMode
	fail  map[string]error
	// run answers `launchctl`/`plutil`; nth is how many identical argvs came
	// before this one. A nil run answers ("", nil).
	run  func(argv string, nth int) (string, error)
	runN map[string]int
}

func newSysRecorder() *sysRecorder {
	return &sysRecorder{
		files: map[string]string{},
		perms: map[string]os.FileMode{},
		fail:  map[string]error{},
		runN:  map[string]int{},
	}
}

func (s *sysRecorder) record(line string) error {
	s.calls = append(s.calls, line)
	return s.fail[line]
}

// runCalls is the subsequence of calls that started a subprocess.
func (s *sysRecorder) runCalls() []string {
	var out []string
	for _, c := range s.calls {
		if strings.HasPrefix(c, "run ") {
			out = append(out, c)
		}
	}
	return out
}

func (s *sysRecorder) count(line string) int {
	n := 0
	for _, c := range s.calls {
		if c == line {
			n++
		}
	}
	return n
}

func (s *sysRecorder) ops() sysOps {
	return sysOps{
		run: func(name string, args ...string) (string, error) {
			argv := strings.Join(append([]string{name}, args...), " ")
			line := "run " + argv
			if err := s.record(line); err != nil {
				return "", err
			}
			nth := s.runN[argv]
			s.runN[argv] = nth + 1
			if s.run == nil {
				return "", nil
			}
			return s.run(argv, nth)
		},
		mkdirAll: func(path string, perm os.FileMode) error {
			return s.record(fmt.Sprintf("mkdir %s %o", path, perm))
		},
		writeFile: func(path string, data []byte, perm os.FileMode) error {
			if err := s.record(fmt.Sprintf("write %s %o", path, perm)); err != nil {
				return err
			}
			s.files[path] = string(data)
			s.perms[path] = perm
			return nil
		},
		readFile: func(path string) ([]byte, error) {
			if err := s.record("read " + path); err != nil {
				return nil, err
			}
			body, ok := s.files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(body), nil
		},
		rename: func(oldpath, newpath string) error {
			if err := s.record("rename " + oldpath + " -> " + newpath); err != nil {
				return err
			}
			s.files[newpath] = s.files[oldpath]
			s.perms[newpath] = s.perms[oldpath]
			delete(s.files, oldpath)
			delete(s.perms, oldpath)
			return nil
		},
		remove: func(path string) error {
			if err := s.record("remove " + path); err != nil {
				return err
			}
			if _, ok := s.files[path]; !ok {
				return os.ErrNotExist
			}
			delete(s.files, path)
			delete(s.perms, path)
			return nil
		},
		chmod: func(path string, mode os.FileMode) error {
			if err := s.record(fmt.Sprintf("chmod %s %o", path, mode)); err != nil {
				return err
			}
			s.perms[path] = mode
			return nil
		},
		statMode: func(path string) (os.FileMode, error) {
			if err := s.record("stat " + path); err != nil {
				return 0, err
			}
			mode, ok := s.perms[path]
			if !ok {
				return 0, os.ErrNotExist
			}
			return mode, nil
		},
		sleep: func(d time.Duration) {
			_ = s.record("sleep " + d.String())
		},
	}
}

// installFixture is the resolved main-instance install every step test acts on.
func installFixture() wardenPaths {
	return wardenPaths{
		root:       "/Users/eva/.officraft",
		home:       "/Users/eva",
		label:      "com.officraft.ocwarden",
		srcExe:     "/tmp/clone/ocwarden",
		ocBase:     "https://oc.example.com",
		ocToken:    jwtWardenOne,
		tokfile:    "/Users/eva/.officraft/warden/exec-warden.tok",
		laDir:      "/Users/eva/Library/LaunchAgents",
		plistPath:  "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
		logDir:     "/Users/eva/.officraft/warden/log",
		binPath:    "/Users/eva/.officraft/warden/ocwarden",
		anchorSrc:  "/tmp/clone/officraft",
		anchorPath: "/Users/eva/.officraft/warden/officraft",
		guiDomain:  "gui/501",
		ocAgentBin: "/Users/eva/.officraft/warden/ocagent",
	}
}

// newRecordingInstaller pairs an installer with the seam and transcript it writes to.
func newRecordingInstaller(dryRun bool) (*installer, *sysRecorder, *bytes.Buffer) {
	sys := newSysRecorder()
	out := &bytes.Buffer{}
	return &installer{out: out, dryRun: dryRun, sys: sys.ops()}, sys, out
}

const minimalPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- RENDERED by ocwarden install for ROOT=/Users/eva/.officraft — do not edit by hand; re-run the installer. -->
<plist version="1.0">
<dict>
    <key>Label</key><string>com.officraft.ocwarden</string>
    <key>ProgramArguments</key>
    <array><string>/Users/eva/.officraft/warden/officraft</string></array>
    <key>WorkingDirectory</key><string>/Users/eva/.officraft</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key><string>/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
        <key>OC_BASE</key><string>https://oc.example.com</string>
        <key>HOME</key><string>/Users/eva</string>
        <key>OC_WARDEN_TOKFILE</key><string>/Users/eva/.officraft/warden/exec-warden.tok</string>
    </dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>/Users/eva/.officraft/warden/log/ocwarden.out.log</string>
    <key>StandardErrorPath</key><string>/Users/eva/.officraft/warden/log/ocwarden.err.log</string>
</dict>
</plist>
`

const stampedPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- RENDERED by ocwarden install for ROOT=/Users/eva/.officraft — do not edit by hand; re-run the installer. -->
<plist version="1.0">
<dict>
    <key>Label</key><string>com.officraft.ocwarden</string>
    <key>ProgramArguments</key>
    <array><string>/Users/eva/.officraft/warden/officraft</string></array>
    <key>WorkingDirectory</key><string>/Users/eva/.officraft</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key><string>/opt/x&amp;y:/bin</string>
        <key>OC_BASE</key><string>https://oc.example.com</string>
        <key>HOME</key><string>/Users/eva</string>
        <key>OC_WARDEN_TOKFILE</key><string>/Users/eva/.officraft/warden/exec-warden.tok</string>
        <key>OC_CLAUDE_BIN</key><string>/opt/claude</string>
        <key>OC_CODEX_BIN</key><string>/opt/codex</string>
    </dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>/Users/eva/.officraft/warden/log/ocwarden.out.log</string>
    <key>StandardErrorPath</key><string>/Users/eva/.officraft/warden/log/ocwarden.err.log</string>
</dict>
</plist>
`

// ---------------------------------------------------------------------------

// runRefusalChild re-execs THIS test binary so a function whose whole contract is
// "kill the process" can be observed from outside it.
func runRefusalChild(t *testing.T, name string) (int, string) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	child.Env = append(os.Environ(), "OCWARDEN_REFUSAL_CHILD=1")
	out, err := child.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		t.Fatalf("run child %s: %v", name, err)
	}
	return exited.ExitCode(), string(out)
}

func refusalText(fn string) string {
	return "\nFATAL: " + fn + " was reached from a test binary.\n" +
		"The real host seam must NEVER be constructed under `go test` — doing so wires the\n" +
		"test process to the LIVE launchd gui domain, where an install/teardown would boot\n" +
		"out this machine's real com.officraft.ocwarden job.\n" +
		"Every entry point must take its effects from newHostSeam(), which TestMain rebinds\n" +
		"to a fake. See hostseam_test.go.\n"
}

func TestRefuseInTestBinary(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		refuseInTestBinary("someSeamBuilder")
		fmt.Println("refuseInTestBinary returned to its caller")
		return
	}
	code, out := runRefusalChild(t, "TestRefuseInTestBinary")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("someSeamBuilder")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("someSeamBuilder"))
	}
	if strings.Contains(out, "refuseInTestBinary returned to its caller") {
		t.Error("refuseInTestBinary returned instead of killing the test binary")
	}
}

func TestRealSysOps(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		s := realSysOps()
		fmt.Printf("realSysOps handed back a seam (run==nil: %v)\n", s.run == nil)
		return
	}
	code, out := runRefusalChild(t, "TestRealSysOps")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("realSysOps")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("realSysOps"))
	}
	if strings.Contains(out, "realSysOps handed back a seam") {
		t.Error("a test binary was handed the real filesystem/launchctl wiring")
	}
}

func TestRealHostSeam(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		h := realHostSeam()
		fmt.Printf("realHostSeam handed back a seam (probe==nil: %v)\n", h.claudeProbe == nil)
		return
	}
	code, out := runRefusalChild(t, "TestRealHostSeam")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("realHostSeam")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("realHostSeam"))
	}
	if strings.Contains(out, "realHostSeam handed back a seam") {
		t.Error("a test binary was handed the real host wiring")
	}
}

func TestSubcmdTag(t *testing.T) {
	t.Skip("a two-line fallback whose only observable effect is the logf/errf prefix, asserted there")
}

func TestLogf(t *testing.T) {
	var out bytes.Buffer
	i := &installer{out: &out}
	i.logf("resolved: ROOT=%s uid=%d", "/Users/eva/.officraft", 501)
	i.logf("plain")

	teardown := &installer{out: &out, tag: "teardown"}
	teardown.logf("removed: %s", "/Users/eva/.officraft/warden/exec-warden.tok")

	want := "[ocwarden install] resolved: ROOT=/Users/eva/.officraft uid=501\n" +
		"[ocwarden install] plain\n" +
		"[ocwarden teardown] removed: /Users/eva/.officraft/warden/exec-warden.tok\n"
	if out.String() != want {
		t.Errorf("out =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestErrf(t *testing.T) {
	var out bytes.Buffer
	i := &installer{out: &out}
	i.errf("OC_TOKEN is required (%s)", "no fallback")

	teardown := &installer{out: &out, tag: "teardown"}
	teardown.errf("refusing")

	want := "[ocwarden install] FATAL: OC_TOKEN is required (no fallback)\n" +
		"[ocwarden teardown] FATAL: refusing\n"
	if out.String() != want {
		t.Errorf("out =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestLabelOrDefault(t *testing.T) {
	t.Skip("a two-line fallback; both branches are asserted through the launchctl argv in TestLaunchctlReinstall and the rendered <Label> in TestRenderPlist")
}

func TestResolvePaths(t *testing.T) {
	main := wardenPaths{
		root:       "/Users/eva/.officraft",
		home:       "/Users/eva",
		namespace:  "",
		label:      "com.officraft.ocwarden",
		srcExe:     "/tmp/clone/bin/ocwarden",
		ocBase:     "https://oc.example.com",
		ocToken:    jwtWardenOne,
		ocID:       "machine-7",
		tokfile:    "/Users/eva/.officraft/warden/exec-warden.tok",
		laDir:      "/Users/eva/Library/LaunchAgents",
		plistPath:  "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
		logDir:     "/Users/eva/.officraft/warden/log",
		binPath:    "/Users/eva/.officraft/warden/ocwarden",
		anchorSrc:  "/tmp/clone/bin/officraft",
		anchorPath: "/Users/eva/.officraft/warden/officraft",
		guiDomain:  "gui/501",
		ocAgentBin: "/Users/eva/.officraft/warden/ocagent",
	}
	namespaced := wardenPaths{
		root:       "/Users/eva/.officraft-lab",
		home:       "/Users/eva",
		namespace:  "lab",
		label:      "com.officraft.ocwarden.lab",
		srcExe:     "/tmp/clone/bin/ocwarden",
		ocBase:     "http://127.0.0.1:7755",
		ocToken:    jwtWardenOne,
		tokfile:    "/Users/eva/.officraft-lab/warden/exec-warden.tok",
		laDir:      "/Users/eva/Library/LaunchAgents",
		plistPath:  "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.lab.plist",
		logDir:     "/Users/eva/.officraft-lab/warden/log",
		binPath:    "/Users/eva/.officraft-lab/warden/ocwarden",
		anchorSrc:  "/tmp/clone/bin/officraft",
		anchorPath: "/Users/eva/.officraft-lab/warden/officraft",
		guiDomain:  "gui/501",
		ocAgentSrc: "/opt/ocagent",
		ocAgentBin: "/Users/eva/.officraft-lab/warden/ocagent",
		credCheck:  "0",
	}

	cases := []struct {
		name string
		env  map[string]string
		want wardenPaths
	}{
		{"main instance", map[string]string{
			"HOME": "/Users/eva", "OC_BASE": "http://oc.example.com/", "OC_TOKEN": jwtWardenOne, "OC_ID": "machine-7",
		}, main},
		{"namespaced instance with a local ocagent and the cred gate opted out", map[string]string{
			"HOME": "/Users/eva", "OC_BASE": "https://127.0.0.1:7755", "OC_TOKEN": jwtWardenOne,
			"OC_NAMESPACE": "lab", "OC_AGENT_BIN": "/opt/ocagent", "OC_CLAUDE_CRED_CHECK": "0",
		}, namespaced},
	}
	for _, c := range cases {
		got, err := resolvePaths(envMap(c.env), "/tmp/clone/bin/ocwarden", 501)
		if err != nil {
			t.Errorf("%s: err = %v, want nil", c.name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: paths =\n%+v\nwant\n%+v", c.name, got, c.want)
		}
	}

	refused := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"HOME unset", map[string]string{"OC_BASE": "https://oc.example.com", "OC_TOKEN": jwtWardenOne},
			"HOME must be set"},
		{"invalid namespace", map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "Lab",
			"OC_BASE": "https://oc.example.com", "OC_TOKEN": jwtWardenOne},
			`OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab"`},
		{"OC_BASE unset", map[string]string{"HOME": "/Users/eva", "OC_TOKEN": jwtWardenOne},
			"OC_BASE is not set — refusing to install a warden that does not know which station to talk to. " +
				"Re-run with OC_BASE set to the station URL (e.g. OC_BASE=https://station.example ocwarden install). " +
				"Guessing http://127.0.0.1:7755 here would be written into this machine's launchd job permanently, " +
				"and members started from it would join whatever is listening there"},
		{"OC_BASE is blank", map[string]string{"HOME": "/Users/eva", "OC_BASE": "   ", "OC_TOKEN": jwtWardenOne},
			"OC_BASE is not set — refusing to install a warden that does not know which station to talk to. " +
				"Re-run with OC_BASE set to the station URL (e.g. OC_BASE=https://station.example ocwarden install). " +
				"Guessing http://127.0.0.1:7755 here would be written into this machine's launchd job permanently, " +
				"and members started from it would join whatever is listening there"},
		{"OC_BASE carries a foreign scheme", map[string]string{"HOME": "/Users/eva", "OC_BASE": "ftp://x", "OC_TOKEN": jwtWardenOne},
			"OC_BASE must be http(s)://host[:port] with no whitespace/XML-special chars, got: ftp://x"},
		{"OC_BASE carries whitespace", map[string]string{"HOME": "/Users/eva", "OC_BASE": "http://a b", "OC_TOKEN": jwtWardenOne},
			"OC_BASE must be http(s)://host[:port] with no whitespace/XML-special chars, got: https://a b"},
		{"OC_TOKEN unset", map[string]string{"HOME": "/Users/eva", "OC_BASE": "https://oc.example.com"},
			"OC_TOKEN is required (the exec-warden member token; NOT the telemetry warden's). " +
				"Usage: OC_BASE=<base> OC_TOKEN=<jwt> [OC_ID=<id>] ocwarden install"},
		{"OC_AGENT_BIN is relative", map[string]string{"HOME": "/Users/eva", "OC_BASE": "https://oc.example.com",
			"OC_TOKEN": jwtWardenOne, "OC_AGENT_BIN": "dist/ocagent"},
			"OC_AGENT_BIN must be an absolute path with no whitespace, got: dist/ocagent"},
		{"OC_AGENT_BIN carries whitespace", map[string]string{"HOME": "/Users/eva", "OC_BASE": "https://oc.example.com",
			"OC_TOKEN": jwtWardenOne, "OC_AGENT_BIN": "/opt/oc agent"},
			"OC_AGENT_BIN must be an absolute path with no whitespace, got: /opt/oc agent"},
	}
	for _, c := range refused {
		got, err := resolvePaths(envMap(c.env), "/tmp/clone/bin/ocwarden", 501)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		if !reflect.DeepEqual(got, wardenPaths{}) {
			t.Errorf("%s: a refusal still derived paths: %+v", c.name, got)
		}
	}
}

func TestRenderPlist(t *testing.T) {
	if got := renderPlist(installFixture()); got != minimalPlist {
		t.Errorf("render =\n%s\nwant\n%s", got, minimalPlist)
	}

	zeroLabel := installFixture()
	zeroLabel.label = ""
	if got := renderPlist(zeroLabel); got != minimalPlist {
		t.Errorf("a zero-value label must render the canonical label; got\n%s", got)
	}

	full := installFixture()
	full.root = "/Users/eva/.officraft-a--b"
	full.namespace = "a--b"
	full.label = "com.officraft.ocwarden.a--b"
	full.anchorPath = "/Users/eva/.officraft-a--b/warden/officraft"
	full.tokfile = "/Users/eva/.officraft-a--b/warden/exec-warden.tok"
	full.logDir = "/Users/eva/.officraft-a--b/warden/log"
	full.claudeBin = "/opt/claude"
	full.codexBin = "/opt/codex"
	full.credCheck = "0"
	full.plistPATH = "/opt/x&y:/bin"

	wantFull := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- RENDERED by ocwarden install for ROOT=/Users/eva/.officraft-a- -b — do not edit by hand; re-run the installer. -->
<plist version="1.0">
<dict>
    <key>Label</key><string>com.officraft.ocwarden.a--b</string>
    <key>ProgramArguments</key>
    <array><string>/Users/eva/.officraft-a--b/warden/officraft</string></array>
    <key>WorkingDirectory</key><string>/Users/eva/.officraft-a--b</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key><string>/opt/x&amp;y:/bin</string>
        <key>OC_BASE</key><string>https://oc.example.com</string>
        <key>HOME</key><string>/Users/eva</string>
        <key>OC_WARDEN_TOKFILE</key><string>/Users/eva/.officraft-a--b/warden/exec-warden.tok</string>
        <key>OC_NAMESPACE</key><string>a--b</string>
        <key>OC_CLAUDE_BIN</key><string>/opt/claude</string>
        <key>OC_CODEX_BIN</key><string>/opt/codex</string>
        <key>OC_CLAUDE_CRED_CHECK</key><string>0</string>
    </dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>/Users/eva/.officraft-a--b/warden/log/ocwarden.out.log</string>
    <key>StandardErrorPath</key><string>/Users/eva/.officraft-a--b/warden/log/ocwarden.err.log</string>
</dict>
</plist>
`
	if got := renderPlist(full); got != wantFull {
		t.Errorf("render =\n%s\nwant\n%s", got, wantFull)
	}
	if err := xmlWellFormed(renderPlist(full)); err != nil {
		t.Errorf("a rendered plist whose root path holds a double dash must stay well-formed: %v", err)
	}
}

func TestRelayedCredCheck(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"0", "0"},
		{" 0 ", "0"},
		{"", ""},
		{"1", ""},
		{"00", ""},
		{"false", ""},
		{"0; <key>x</key>", ""},
	}
	for _, c := range cases {
		env := envMap(map[string]string{"OC_CLAUDE_CRED_CHECK": c.raw})
		if got := relayedCredCheck(env); got != c.want {
			t.Errorf("relayedCredCheck(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestXmlEscape(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"/opt/homebrew/bin", "/opt/homebrew/bin"},
		{"a&b", "a&amp;b"},
		{"a<b>c", "a&lt;b&gt;c"},
		{`say "hi"`, "say &quot;hi&quot;"},
		{"it's", "it&apos;s"},
		{`&<>"'`, "&amp;&lt;&gt;&quot;&apos;"},
		{"", ""},
	}
	for _, c := range cases {
		if got := xmlEscape(c.raw); got != c.want {
			t.Errorf("xmlEscape(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestXmlWellFormed(t *testing.T) {
	if err := xmlWellFormed(minimalPlist); err != nil {
		t.Errorf("the rendered plist must parse as XML: %v", err)
	}
	if err := xmlWellFormed(`<a><b/></a>`); err != nil {
		t.Errorf("well-formed XML returned %v, want nil", err)
	}
	malformed := []string{
		`<plist><dict><key>a</key></plist>`,
		`<string>a<b</string>`,
		`<a>`,
	}
	for _, doc := range malformed {
		if err := xmlWellFormed(doc); err == nil {
			t.Errorf("xmlWellFormed(%q) = nil, want an error", doc)
		}
	}
}

func TestResolveClaudeForInstall(t *testing.T) {
	env := envMap(map[string]string{"HOME": "/Users/eva", "PATH": "/Users/eva/.asdf/shims:/usr/bin"})

	cases := []struct {
		name      string
		env       func(string) string
		lookup    func() string
		probe     func(bin, pathEnv, home string) error
		wantBin   string
		wantPATH  string
		wantLog   string
		wantProbe []string
	}{
		{
			name: "no claude anywhere", env: env,
			lookup: func() string { return "" },
			probe:  func(string, string, string) error { return nil },
		},
		{
			name: "a relative candidate is not stampable", env: env,
			lookup:  func() string { return "bin/claude" },
			probe:   func(string, string, string) error { return nil },
			wantLog: `WARN: resolved claude path "bin/claude" is not stampable (must be absolute, no whitespace/XML-special chars) — not stamping OC_CLAUDE_BIN` + "\n",
		},
		{
			name: "a candidate with XML-special characters is not stampable", env: env,
			lookup:  func() string { return "/opt/cl&ude" },
			probe:   func(string, string, string) error { return nil },
			wantLog: `WARN: resolved claude path "/opt/cl&ude" is not stampable (must be absolute, no whitespace/XML-special chars) — not stamping OC_CLAUDE_BIN` + "\n",
		},
		{
			name: "no probe seam stamps the candidate unverified", env: env,
			lookup:  func() string { return "/opt/homebrew/bin/claude" },
			probe:   nil,
			wantBin: "/opt/homebrew/bin/claude",
		},
		{
			name: "it runs under the minimal launchd PATH", env: env,
			lookup:    func() string { return "/opt/homebrew/bin/claude" },
			probe:     func(string, string, string) error { return nil },
			wantBin:   "/opt/homebrew/bin/claude",
			wantProbe: []string{"/opt/homebrew/bin/claude|" + wardenPlistPATH + "|/Users/eva"},
		},
		{
			name: "a shim needs the installer PATH carried into the plist", env: env,
			lookup: func() string { return "/Users/eva/.asdf/shims/claude" },
			probe: func(_, pathEnv, _ string) error {
				if pathEnv == wardenPlistPATH {
					return errors.New("env: node: No such file or directory")
				}
				return nil
			},
			wantBin:  "/Users/eva/.asdf/shims/claude",
			wantPATH: "/Users/eva/.asdf/shims:/usr/bin",
			wantLog: "claude at /Users/eva/.asdf/shims/claude needs the installer's PATH to run " +
				"(version-manager shim / env-shebang) — stamping the full installer PATH into the warden plist alongside OC_CLAUDE_BIN\n",
			wantProbe: []string{
				"/Users/eva/.asdf/shims/claude|" + wardenPlistPATH + "|/Users/eva",
				"/Users/eva/.asdf/shims/claude|/Users/eva/.asdf/shims:/usr/bin|/Users/eva",
			},
		},
		{
			name: "it fails both probes and is stamped best-effort", env: env,
			lookup:  func() string { return "/opt/homebrew/bin/claude" },
			probe:   func(string, string, string) error { return errors.New("exit status 1") },
			wantBin: "/opt/homebrew/bin/claude",
			wantLog: "WARN: claude at /opt/homebrew/bin/claude failed `--version` under both the minimal launchd PATH " +
				"and the installer PATH — stamping OC_CLAUDE_BIN best-effort; spawns may still fail (check that claude runs headless)\n",
			wantProbe: []string{
				"/opt/homebrew/bin/claude|" + wardenPlistPATH + "|/Users/eva",
				"/opt/homebrew/bin/claude|/Users/eva/.asdf/shims:/usr/bin|/Users/eva",
			},
		},
		{
			name:    "an empty installer PATH leaves nothing to fall back on",
			env:     envMap(map[string]string{"HOME": "/Users/eva"}),
			lookup:  func() string { return "/opt/homebrew/bin/claude" },
			probe:   func(string, string, string) error { return errors.New("exit status 1") },
			wantBin: "/opt/homebrew/bin/claude",
			wantLog: "WARN: claude at /opt/homebrew/bin/claude failed `--version` under both the minimal launchd PATH " +
				"and the installer PATH — stamping OC_CLAUDE_BIN best-effort; spawns may still fail (check that claude runs headless)\n",
			wantProbe: []string{"/opt/homebrew/bin/claude|" + wardenPlistPATH + "|/Users/eva"},
		},
	}

	for _, c := range cases {
		var log bytes.Buffer
		var probed []string
		probe := c.probe
		if probe != nil {
			probe = func(bin, pathEnv, home string) error {
				probed = append(probed, bin+"|"+pathEnv+"|"+home)
				return c.probe(bin, pathEnv, home)
			}
		}
		logf := func(format string, a ...any) { fmt.Fprintf(&log, format+"\n", a...) }

		bin, plistPATH := resolveClaudeForInstall(c.env, c.lookup, probe, logf)
		if bin != c.wantBin || plistPATH != c.wantPATH {
			t.Errorf("%s: resolveClaudeForInstall = (%q, %q), want (%q, %q)", c.name, bin, plistPATH, c.wantBin, c.wantPATH)
		}
		if log.String() != c.wantLog {
			t.Errorf("%s: log = %q, want %q", c.name, log.String(), c.wantLog)
		}
		if !reflect.DeepEqual(probed, c.wantProbe) {
			t.Errorf("%s: probes = %v, want %v", c.name, probed, c.wantProbe)
		}
	}
}

func TestRealClaudeProbe(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$HOME/argv\"\nenv > \"$HOME/env\"\nexit ${PROBE_EXIT:-0}\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("stage the probe target: %v", err)
	}
	t.Setenv("OC_PROBE_MARKER", "inherited-from-the-installer")

	if err := realClaudeProbe(bin, "/opt/homebrew/bin:/usr/bin:/bin", home); err != nil {
		t.Fatalf("probe of a claude that exits 0 = %v, want nil", err)
	}
	argv, err := os.ReadFile(filepath.Join(home, "argv"))
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	if string(argv) != "--version\n" {
		t.Errorf("argv = %q, want %q", argv, "--version\n")
	}
	childEnv, err := os.ReadFile(filepath.Join(home, "env"))
	if err != nil {
		t.Fatalf("read recorded env: %v", err)
	}
	for _, want := range []string{"PATH=/opt/homebrew/bin:/usr/bin:/bin\n", "HOME=" + home + "\n"} {
		if !strings.Contains(string(childEnv), want) {
			t.Errorf("child env =\n%s\nwant it to contain %q", childEnv, want)
		}
	}
	if strings.Contains(string(childEnv), "inherited-from-the-installer") {
		t.Errorf("the probe leaked the installer's own environment:\n%s", childEnv)
	}

	failing := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("stage the failing probe target: %v", err)
	}
	if err := realClaudeProbe(failing, "/usr/bin:/bin", home); err == nil || err.Error() != "exit status 3" {
		t.Errorf("probe of a claude that exits 3 = %v, want \"exit status 3\"", err)
	}
	if err := realClaudeProbe(filepath.Join(home, "not-here"), "/usr/bin:/bin", home); err == nil {
		t.Error("probe of a missing binary = nil, want an error")
	}
	notExec := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("stage the non-executable probe target: %v", err)
	}
	if err := realClaudeProbe(notExec, "/usr/bin:/bin", home); err == nil {
		t.Error("probe of a non-executable file = nil, want an error")
	}
}

func TestGuard(t *testing.T) {
	p := installFixture()
	namespaced := installFixture()
	namespaced.namespace = "lab"

	cases := []struct {
		name      string
		paths     wardenPaths
		force     bool
		installed string
		wantErr   string
		wantLog   string
		wantCalls []string
	}{
		{
			name: "no tokfile is a fresh install", paths: p,
			wantCalls: []string{"read /Users/eva/.officraft/warden/exec-warden.tok"},
		},
		{
			name: "--force never even looks", paths: p, force: true, installed: jwtWardenTwo,
			wantLog: "[ocwarden install] --force: skipping one-warden-per-machine guard\n",
		},
		{
			name: "the same machine re-provisions", paths: p, installed: jwtWardenOne + "\n",
			wantLog:   "[ocwarden install] guard: existing warden is the same machine (warden-1) — idempotent re-provision\n",
			wantCalls: []string{"read /Users/eva/.officraft/warden/exec-warden.tok"},
		},
		{
			name: "an unidentifiable tokfile does not block a re-provision", paths: p, installed: jwtNoSub,
			wantCalls: []string{"read /Users/eva/.officraft/warden/exec-warden.tok"},
		},
		{
			name: "another machine's warden is refused", paths: p, installed: jwtWardenTwo,
			wantErr: "refusing: a warden for machine warden-2 is already installed on this box; " +
				"run 'ocwarden teardown --canonical' first, or pass --force to replace",
			wantCalls: []string{"read /Users/eva/.officraft/warden/exec-warden.tok"},
		},
		{
			name: "a namespaced install is told its own teardown command", paths: namespaced, installed: jwtWardenTwo,
			wantErr: "refusing: a warden for machine warden-2 is already installed on this box; " +
				"run 'OC_NAMESPACE=lab ocwarden teardown' first, or pass --force to replace",
			wantCalls: []string{"read /Users/eva/.officraft/warden/exec-warden.tok"},
		},
	}
	for _, c := range cases {
		i, sys, out := newRecordingInstaller(false)
		i.force = c.force
		if c.installed != "" {
			sys.files[c.paths.tokfile] = c.installed
		}
		err := i.guard(c.paths)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", c.name, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
		if out.String() != c.wantLog {
			t.Errorf("%s: log = %q, want %q", c.name, out.String(), c.wantLog)
		}
		if !reflect.DeepEqual(sys.calls, c.wantCalls) {
			t.Errorf("%s: calls = %v, want %v", c.name, sys.calls, c.wantCalls)
		}
	}

	i, sys, _ := newRecordingInstaller(false)
	sys.files[p.tokfile] = jwtWardenTwo
	if err := i.guard(p); err == nil {
		t.Fatal("a foreign warden must be refused")
	}
	if !reflect.DeepEqual(sys.calls, []string{"read /Users/eva/.officraft/warden/exec-warden.tok"}) {
		t.Errorf("the refusal mutated the machine: %v", sys.calls)
	}
}

func TestCopyBinary(t *testing.T) {
	tmp := fmt.Sprintf("/Users/eva/.officraft/warden/.ocwarden.%d", os.Getpid())

	i, sys, out := newRecordingInstaller(false)
	sys.files["/tmp/clone/ocwarden"] = "OCWARDEN-BYTES"
	if err := i.copyBinary(installFixture()); err != nil {
		t.Fatalf("copyBinary: %v", err)
	}
	wantCalls := []string{
		"mkdir /Users/eva/.officraft/warden 755",
		"read /tmp/clone/ocwarden",
		"write " + tmp + " 755",
		"chmod " + tmp + " 755",
		"rename " + tmp + " -> /Users/eva/.officraft/warden/ocwarden",
	}
	if !reflect.DeepEqual(sys.calls, wantCalls) {
		t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
	}
	if sys.files["/Users/eva/.officraft/warden/ocwarden"] != "OCWARDEN-BYTES" {
		t.Errorf("installed bytes = %q, want %q", sys.files["/Users/eva/.officraft/warden/ocwarden"], "OCWARDEN-BYTES")
	}
	if mode := sys.perms["/Users/eva/.officraft/warden/ocwarden"]; mode != 0o755 {
		t.Errorf("installed mode = %o, want 755", mode)
	}
	if out.String() != "[ocwarden install] installed binary (0755): /Users/eva/.officraft/warden/ocwarden\n" {
		t.Errorf("log = %q", out.String())
	}

	inPlace := installFixture()
	inPlace.srcExe = inPlace.binPath
	i, sys, out = newRecordingInstaller(false)
	if err := i.copyBinary(inPlace); err != nil {
		t.Fatalf("copyBinary in place: %v", err)
	}
	if len(sys.calls) != 0 {
		t.Errorf("a re-run from the installed location copied anyway: %v", sys.calls)
	}
	if want := "[ocwarden install] running binary is already the installed home binary (/Users/eva/.officraft/warden/ocwarden); skipping self-copy\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	i, sys, out = newRecordingInstaller(true)
	if err := i.copyBinary(installFixture()); err != nil {
		t.Fatalf("copyBinary dry run: %v", err)
	}
	if len(sys.calls) != 0 {
		t.Errorf("a dry run touched the machine: %v", sys.calls)
	}
	if want := "[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; copy /tmp/clone/ocwarden -> a 0755 temp then atomic rename -> /Users/eva/.officraft/warden/ocwarden\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	i, sys, _ = newRecordingInstaller(false)
	err := i.copyBinary(installFixture())
	if err == nil || err.Error() != "read own binary /tmp/clone/ocwarden: file does not exist" {
		t.Errorf("err = %v, want \"read own binary /tmp/clone/ocwarden: file does not exist\"", err)
	}
	if _, installed := sys.files["/Users/eva/.officraft/warden/ocwarden"]; installed {
		t.Error("an unreadable source still installed a binary")
	}
}

func TestCopyAnchorIfAbsent(t *testing.T) {
	prev := embeddedAnchor
	t.Cleanup(func() { embeddedAnchor = prev })
	embeddedAnchor = func() []byte { return []byte("EMBEDDED-ANCHOR") }

	i, sys, out := newRecordingInstaller(false)
	sys.files["/Users/eva/.officraft/warden/officraft"] = "THE-TCC-IDENTITY"
	sys.files["/tmp/clone/officraft"] = "A-DIFFERENT-ANCHOR"
	if err := i.copyAnchorIfAbsent(installFixture()); err != nil {
		t.Fatalf("copyAnchorIfAbsent: %v", err)
	}
	if !reflect.DeepEqual(sys.calls, []string{"read /Users/eva/.officraft/warden/officraft"}) {
		t.Errorf("an existing anchor was not preserved: %v", sys.calls)
	}
	if sys.files["/Users/eva/.officraft/warden/officraft"] != "THE-TCC-IDENTITY" {
		t.Errorf("the anchor bytes changed: %q", sys.files["/Users/eva/.officraft/warden/officraft"])
	}
	if want := "[ocwarden install] fixed identity anchor already exists; preserving /Users/eva/.officraft/warden/officraft\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	i, sys, out = newRecordingInstaller(false)
	sys.files["/tmp/clone/officraft"] = "SIBLING-ANCHOR"
	if err := i.copyAnchorIfAbsent(installFixture()); err != nil {
		t.Fatalf("copyAnchorIfAbsent: %v", err)
	}
	wantCalls := []string{
		"read /Users/eva/.officraft/warden/officraft",
		"mkdir /Users/eva/.officraft/warden 755",
		"read /tmp/clone/officraft",
		"write /Users/eva/.officraft/warden/officraft 755",
		"chmod /Users/eva/.officraft/warden/officraft 755",
	}
	if !reflect.DeepEqual(sys.calls, wantCalls) {
		t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
	}
	if sys.files["/Users/eva/.officraft/warden/officraft"] != "SIBLING-ANCHOR" {
		t.Errorf("installed anchor = %q, want %q", sys.files["/Users/eva/.officraft/warden/officraft"], "SIBLING-ANCHOR")
	}
	if want := "[ocwarden install] installed fixed identity anchor (0755): /Users/eva/.officraft/warden/officraft\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	for _, sibling := range []struct {
		name string
		body string
		have bool
	}{
		{"no anchor beside ocwarden", "", false},
		{"a zero-byte sibling is not an anchor", "", true},
	} {
		i, sys, out = newRecordingInstaller(false)
		if sibling.have {
			sys.files["/tmp/clone/officraft"] = sibling.body
		}
		if err := i.copyAnchorIfAbsent(installFixture()); err != nil {
			t.Fatalf("%s: copyAnchorIfAbsent: %v", sibling.name, err)
		}
		if sys.files["/Users/eva/.officraft/warden/officraft"] != "EMBEDDED-ANCHOR" {
			t.Errorf("%s: installed anchor = %q, want the embedded copy", sibling.name, sys.files["/Users/eva/.officraft/warden/officraft"])
		}
		want := "[ocwarden install] no anchor beside ocwarden; using the copy embedded in this binary\n" +
			"[ocwarden install] installed fixed identity anchor (0755): /Users/eva/.officraft/warden/officraft\n"
		if out.String() != want {
			t.Errorf("%s: log = %q, want %q", sibling.name, out.String(), want)
		}
	}

	embeddedAnchor = func() []byte { return nil }
	i, sys, _ = newRecordingInstaller(false)
	err := i.copyAnchorIfAbsent(installFixture())
	wantErr := "read fixed identity anchor /tmp/clone/officraft (and this ocwarden carries no embedded anchor): file does not exist"
	if err == nil || err.Error() != wantErr {
		t.Errorf("err = %v, want %q", err, wantErr)
	}
	if _, wrote := sys.files["/Users/eva/.officraft/warden/officraft"]; wrote {
		t.Error("an anchorless install still wrote an anchor")
	}

	i, sys, _ = newRecordingInstaller(false)
	sys.fail["read /Users/eva/.officraft/warden/officraft"] = errors.New("permission denied")
	err = i.copyAnchorIfAbsent(installFixture())
	wantErr = "check fixed identity anchor /Users/eva/.officraft/warden/officraft: permission denied"
	if err == nil || err.Error() != wantErr {
		t.Errorf("err = %v, want %q", err, wantErr)
	}
	if len(sys.calls) != 1 {
		t.Errorf("an unreadable anchor path kept going: %v", sys.calls)
	}

	i, sys, out = newRecordingInstaller(true)
	if err := i.copyAnchorIfAbsent(installFixture()); err != nil {
		t.Fatalf("copyAnchorIfAbsent dry run: %v", err)
	}
	if !reflect.DeepEqual(sys.calls, []string{"read /Users/eva/.officraft/warden/officraft"}) {
		t.Errorf("a dry run mutated the machine: %v", sys.calls)
	}
	if want := "[ocwarden install] DRYRUN would: copy fixed identity anchor /tmp/clone/officraft -> /Users/eva/.officraft/warden/officraft only if absent\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}
}

func TestInstallOcAgent(t *testing.T) {
	tmp := fmt.Sprintf("/Users/eva/.officraft/warden/.ocagent.%d", os.Getpid())
	download := installFixture()
	local := installFixture()
	local.ocAgentSrc = "/opt/ocagent"

	t.Run("a local override is copied without a probe", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.files["/opt/ocagent"] = "LOCAL-OCAGENT"
		probed := 0
		i.agentProbe = func(string) error { probed++; return nil }
		i.agentGet = func(string) (int, []byte, error) {
			t.Error("a local override still went to the server")
			return 0, nil, nil
		}
		if err := i.installOcAgent(local); err != nil {
			t.Fatalf("installOcAgent: %v", err)
		}
		wantCalls := []string{
			"read /opt/ocagent",
			"mkdir /Users/eva/.officraft/warden 755",
			"write " + tmp + " 755",
			"chmod " + tmp + " 755",
			"rename " + tmp + " -> /Users/eva/.officraft/warden/ocagent",
		}
		if !reflect.DeepEqual(sys.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
		}
		if sys.files["/Users/eva/.officraft/warden/ocagent"] != "LOCAL-OCAGENT" {
			t.Errorf("installed bytes = %q", sys.files["/Users/eva/.officraft/warden/ocagent"])
		}
		if mode := sys.perms["/Users/eva/.officraft/warden/ocagent"]; mode != 0o755 {
			t.Errorf("installed mode = %o, want 755", mode)
		}
		if probed != 0 {
			t.Errorf("the local override was probed %d times, want 0", probed)
		}
		if want := "[ocwarden install] installed ocagent (0755): /Users/eva/.officraft/warden/ocagent\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("the download is verified before it is installed", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		var asked []string
		i.agentGet = func(path string) (int, []byte, error) {
			asked = append(asked, path)
			return 200, []byte("DOWNLOADED-OCAGENT"), nil
		}
		var probedPath string
		i.agentProbe = func(bin string) error { probedPath = bin; return nil }
		if err := i.installOcAgent(download); err != nil {
			t.Fatalf("installOcAgent: %v", err)
		}
		if !reflect.DeepEqual(asked, []string{"/api/agent/binary"}) {
			t.Errorf("requested %v, want [/api/agent/binary]", asked)
		}
		wantCalls := []string{
			"mkdir /Users/eva/.officraft/warden 755",
			"write " + tmp + " 755",
			"chmod " + tmp + " 755",
			"rename " + tmp + " -> /Users/eva/.officraft/warden/ocagent",
		}
		if !reflect.DeepEqual(sys.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
		}
		if probedPath != tmp {
			t.Errorf("probed %q, want the temp %q — the verify must happen before the swap", probedPath, tmp)
		}
		if sys.files["/Users/eva/.officraft/warden/ocagent"] != "DOWNLOADED-OCAGENT" {
			t.Errorf("installed bytes = %q", sys.files["/Users/eva/.officraft/warden/ocagent"])
		}
		want := "[ocwarden install] downloading ocagent from https://oc.example.com/api/agent/binary ...\n" +
			"[ocwarden install] installed ocagent (0755): /Users/eva/.officraft/warden/ocagent\n"
		if out.String() != want {
			t.Errorf("log =\n%s\nwant\n%s", out.String(), want)
		}
	})

	t.Run("a download that will not exec is discarded", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		i.agentGet = func(string) (int, []byte, error) { return 200, []byte("TRUNCATED"), nil }
		i.agentProbe = func(string) error { return errors.New("exec format error") }
		err := i.installOcAgent(download)
		wantErr := "downloaded ocagent failed verify — not installing (would brick spawn): exec format error"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if last := sys.calls[len(sys.calls)-1]; last != "remove "+tmp {
			t.Errorf("last call = %q, want the temp to be removed", last)
		}
		if _, installed := sys.files["/Users/eva/.officraft/warden/ocagent"]; installed {
			t.Error("a failed verify still installed an ocagent")
		}
	})

	t.Run("no source at all", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		err := i.installOcAgent(download)
		wantErr := "no ocagent source: OC_AGENT_BIN unset and no download getter wired"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if len(sys.calls) != 0 {
			t.Errorf("calls = %v, want none", sys.calls)
		}
	})

	t.Run("the source is already the installed sibling", func(t *testing.T) {
		inPlace := installFixture()
		inPlace.ocAgentSrc = inPlace.ocAgentBin
		i, sys, out := newRecordingInstaller(false)
		if err := i.installOcAgent(inPlace); err != nil {
			t.Fatalf("installOcAgent: %v", err)
		}
		if len(sys.calls) != 0 {
			t.Errorf("calls = %v, want none", sys.calls)
		}
		if want := "[ocwarden install] ocagent source is already the installed home binary (/Users/eva/.officraft/warden/ocagent); skipping self-copy\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("a dry run neither downloads nor writes", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		i.agentGet = func(string) (int, []byte, error) {
			t.Error("a dry run reached the server")
			return 0, nil, nil
		}
		if err := i.installOcAgent(download); err != nil {
			t.Fatalf("installOcAgent: %v", err)
		}
		if len(sys.calls) != 0 {
			t.Errorf("calls = %v, want none", sys.calls)
		}
		want := "[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; download ocagent from " +
			"https://oc.example.com/api/agent/binary -> verify-exec -> atomic rename -> /Users/eva/.officraft/warden/ocagent\n"
		if out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}

		i, sys, out = newRecordingInstaller(true)
		if err := i.installOcAgent(local); err != nil {
			t.Fatalf("installOcAgent: %v", err)
		}
		if len(sys.calls) != 0 {
			t.Errorf("calls = %v, want none", sys.calls)
		}
		want = "[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; copy (OC_AGENT_BIN override) " +
			"/opt/ocagent -> a 0755 temp then atomic rename -> /Users/eva/.officraft/warden/ocagent\n"
		if out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})
}

func TestDownloadOcAgent(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     []byte
		getErr   error
		wantBody string
		wantErr  string
	}{
		{"a served binary", 200, []byte("OCAGENT-BYTES"), nil, "OCAGENT-BYTES", ""},
		{"transport failure", 0, nil, errors.New("dial tcp: connection refused"), "",
			"download ocagent from https://oc.example.com/api/agent/binary: dial tcp: connection refused"},
		{"not found", 404, []byte("no such route"), nil, "",
			"download ocagent from https://oc.example.com/api/agent/binary: status 404"},
		{"server error", 500, []byte("boom"), nil, "",
			"download ocagent from https://oc.example.com/api/agent/binary: status 500"},
		{"an empty 200 is not a binary", 200, nil, nil, "",
			"download ocagent from https://oc.example.com/api/agent/binary: empty body"},
	}
	for _, c := range cases {
		i, _, out := newRecordingInstaller(false)
		var asked []string
		i.agentGet = func(path string) (int, []byte, error) {
			asked = append(asked, path)
			return c.status, c.body, c.getErr
		}
		body, err := i.downloadOcAgent(installFixture())
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", c.name, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
		if string(body) != c.wantBody {
			t.Errorf("%s: body = %q, want %q", c.name, body, c.wantBody)
		}
		if !reflect.DeepEqual(asked, []string{"/api/agent/binary"}) {
			t.Errorf("%s: requested %v, want [/api/agent/binary]", c.name, asked)
		}
		if want := "[ocwarden install] downloading ocagent from https://oc.example.com/api/agent/binary ...\n"; out.String() != want {
			t.Errorf("%s: log = %q, want %q", c.name, out.String(), want)
		}
	}
}

func TestTokfileWriter(t *testing.T) {
	dst := "/Users/eva/.officraft/warden/exec-warden.tok"
	tmp := fmt.Sprintf("/Users/eva/.officraft/warden/.exec-warden.tok.%d", os.Getpid())

	sys := newSysRecorder()
	if err := sys.ops().tokfileWriter().write(dst, jwtWardenOne); err != nil {
		t.Fatalf("write: %v", err)
	}
	wantCalls := []string{
		"mkdir /Users/eva/.officraft/warden 700",
		"write " + tmp + " 600",
		"chmod " + tmp + " 600",
		"stat " + tmp,
		"rename " + tmp + " -> " + dst,
	}
	if !reflect.DeepEqual(sys.calls, wantCalls) {
		t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
	}
	if sys.files[dst] != jwtWardenOne {
		t.Errorf("written credential = %q, want the one it was handed", sys.files[dst])
	}

	failing := newSysRecorder()
	failing.fail["rename "+tmp+" -> "+dst] = errors.New("cross-device link")
	if err := failing.ops().tokfileWriter().write(dst, jwtWardenOne); err == nil {
		t.Fatal("write = nil, want the rename failure")
	}
	if last := failing.calls[len(failing.calls)-1]; last != "remove "+tmp {
		t.Errorf("last call = %q, want the temp removed through the same seam", last)
	}
}

func TestOsTokfileWriter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "warden")
	dst := filepath.Join(dir, "exec-warden.tok")

	if err := osTokfileWriter().write(dst, jwtWardenOne); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != jwtWardenOne {
		t.Errorf("file holds %q, want the credential it was handed", body)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("dir perms = %o, want 700", dirInfo.Mode().Perm())
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(left) != 1 || left[0].Name() != "exec-warden.tok" {
		t.Errorf("directory holds %v, want only exec-warden.tok", left)
	}

	blocked := filepath.Join(dir, "occupied")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("stage the blocked destination: %v", err)
	}
	if err := osTokfileWriter().write(blocked, jwtWardenOne); err == nil {
		t.Fatal("write onto a directory = nil, want an error")
	}
	left, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(left) != 2 {
		t.Errorf("a failed write left %v behind, want only the tokfile and the blocked dir", left)
	}
}

func TestWrite(t *testing.T) {
	dst := "/w/exec-warden.tok"
	tmp := fmt.Sprintf("/w/.exec-warden.tok.%d", os.Getpid())

	type seam struct {
		calls   []string
		written map[string]string
		removed []string
	}

	build := func(s *seam, mode os.FileMode, fail map[string]error) tokfileWriter {
		s.written = map[string]string{}
		note := func(line string) error {
			s.calls = append(s.calls, line)
			return fail[line]
		}
		return tokfileWriter{
			mkdirAll: func(p string, perm os.FileMode) error { return note(fmt.Sprintf("mkdir %s %o", p, perm)) },
			writeFile: func(p string, data []byte, perm os.FileMode) error {
				if err := note(fmt.Sprintf("write %s %o", p, perm)); err != nil {
					return err
				}
				s.written[p] = string(data)
				return nil
			},
			chmod:    func(p string, m os.FileMode) error { return note(fmt.Sprintf("chmod %s %o", p, m)) },
			statMode: func(p string) (os.FileMode, error) { return mode, note("stat " + p) },
			rename: func(o, n string) error {
				if err := note("rename " + o + " -> " + n); err != nil {
					return err
				}
				s.written[n] = s.written[o]
				delete(s.written, o)
				return nil
			},
			remove: func(p string) error { s.removed = append(s.removed, p); return nil },
		}
	}

	var ok seam
	if err := build(&ok, 0o600, nil).write(dst, jwtWardenOne); err != nil {
		t.Fatalf("write: %v", err)
	}
	wantCalls := []string{
		"mkdir /w 700",
		"write " + tmp + " 600",
		"chmod " + tmp + " 600",
		"stat " + tmp,
		"rename " + tmp + " -> " + dst,
	}
	if !reflect.DeepEqual(ok.calls, wantCalls) {
		t.Errorf("calls =\n%v\nwant\n%v", ok.calls, wantCalls)
	}
	if ok.written[dst] != jwtWardenOne {
		t.Errorf("destination holds %q, want the credential it was handed", ok.written[dst])
	}
	if len(ok.removed) != 0 {
		t.Errorf("a successful write removed %v", ok.removed)
	}

	cases := []struct {
		name        string
		mode        os.FileMode
		fail        map[string]error
		wantErr     string
		wantCalls   []string
		wantRemoved []string
	}{
		{
			name: "the directory cannot be made", mode: 0o600,
			fail:      map[string]error{"mkdir /w 700": errors.New("read-only file system")},
			wantErr:   "mkdir tokfile dir /w: read-only file system",
			wantCalls: []string{"mkdir /w 700"},
		},
		{
			name: "the temp cannot be written", mode: 0o600,
			fail:      map[string]error{"write " + tmp + " 600": errors.New("no space left on device")},
			wantErr:   "write temp tokfile " + tmp + ": no space left on device",
			wantCalls: []string{"mkdir /w 700", "write " + tmp + " 600"},
		},
		{
			name: "the temp cannot be chmodded", mode: 0o600,
			fail:      map[string]error{"chmod " + tmp + " 600": errors.New("operation not permitted")},
			wantErr:   "chmod temp tokfile " + tmp + ": operation not permitted",
			wantCalls: []string{"mkdir /w 700", "write " + tmp + " 600", "chmod " + tmp + " 600"},
		},
		{
			name: "the temp cannot be stat'd", mode: 0o600,
			fail:        map[string]error{"stat " + tmp: errors.New("no such file or directory")},
			wantErr:     "stat temp tokfile " + tmp + ": no such file or directory",
			wantCalls:   []string{"mkdir /w 700", "write " + tmp + " 600", "chmod " + tmp + " 600", "stat " + tmp},
			wantRemoved: []string{tmp},
		},
		{
			name: "the temp is not 0600", mode: 0o644,
			wantErr:     "temp tokfile perms are not 0600: " + tmp + " (got 644)",
			wantCalls:   []string{"mkdir /w 700", "write " + tmp + " 600", "chmod " + tmp + " 600", "stat " + tmp},
			wantRemoved: []string{tmp},
		},
		{
			name: "the rename fails", mode: 0o600,
			fail:    map[string]error{"rename " + tmp + " -> " + dst: errors.New("cross-device link")},
			wantErr: "atomic rename tokfile -> " + dst + ": cross-device link",
			wantCalls: []string{"mkdir /w 700", "write " + tmp + " 600", "chmod " + tmp + " 600",
				"stat " + tmp, "rename " + tmp + " -> " + dst},
			wantRemoved: []string{tmp},
		},
	}
	for _, c := range cases {
		var s seam
		w := build(&s, c.mode, c.fail)
		err := w.write(dst, jwtWardenOne)
		if err == nil || err.Error() != c.wantErr {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
		if !reflect.DeepEqual(s.calls, c.wantCalls) {
			t.Errorf("%s: calls =\n%v\nwant\n%v", c.name, s.calls, c.wantCalls)
		}
		if !reflect.DeepEqual(s.removed, c.wantRemoved) {
			t.Errorf("%s: removed = %v, want %v", c.name, s.removed, c.wantRemoved)
		}
		if _, replaced := s.written[dst]; replaced {
			t.Errorf("%s: a failed write replaced the live credential", c.name)
		}
	}

	var noRemove seam
	w := build(&noRemove, 0o644, nil)
	w.remove = nil
	if err := w.write(dst, jwtWardenOne); err == nil || err.Error() != "temp tokfile perms are not 0600: "+tmp+" (got 644)" {
		t.Errorf("err = %v, want the perms refusal", err)
	}
}

func TestCleanup(t *testing.T) {
	t.Skip("a one-line optional remove; both the wired and the nil seam are asserted through TestWrite's failure paths")
}

func TestWriteTokfile(t *testing.T) {
	p := installFixture()
	tmp := fmt.Sprintf("/Users/eva/.officraft/warden/.exec-warden.tok.%d", os.Getpid())

	i, sys, out := newRecordingInstaller(false)
	if err := i.writeTokfile(p); err != nil {
		t.Fatalf("writeTokfile: %v", err)
	}
	if sys.files[p.tokfile] != jwtWardenOne {
		t.Errorf("tokfile holds %q, want the resolved credential", sys.files[p.tokfile])
	}
	if mode := sys.perms[p.tokfile]; mode != 0o600 {
		t.Errorf("tokfile mode = %o, want 600", mode)
	}
	if want := "[ocwarden install] wrote tokfile (0600): /Users/eva/.officraft/warden/exec-warden.tok\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	i, sys, out = newRecordingInstaller(true)
	if err := i.writeTokfile(p); err != nil {
		t.Fatalf("writeTokfile dry run: %v", err)
	}
	if len(sys.calls) != 0 {
		t.Errorf("a dry run wrote %v", sys.calls)
	}
	want := "[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; write <token> to a 0600 temp then atomic rename -> /Users/eva/.officraft/warden/exec-warden.tok\n"
	if out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	i, sys, out = newRecordingInstaller(false)
	sys.fail["write "+tmp+" 600"] = errors.New("no space left on device")
	err := i.writeTokfile(p)
	if err == nil || err.Error() != "write temp tokfile "+tmp+": no space left on device" {
		t.Errorf("err = %v, want the underlying write failure", err)
	}
	if out.String() != "" {
		t.Errorf("a failed write still logged %q", out.String())
	}
}

func TestWritePlist(t *testing.T) {
	p := installFixture()

	i, sys, out := newRecordingInstaller(false)
	if err := i.writePlist(p); err != nil {
		t.Fatalf("writePlist: %v", err)
	}
	wantCalls := []string{
		"mkdir /Users/eva/Library/LaunchAgents 755",
		"write /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist 644",
		"run plutil -lint /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
	}
	if !reflect.DeepEqual(sys.calls, wantCalls) {
		t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
	}
	if sys.files[p.plistPath] != minimalPlist {
		t.Errorf("written plist =\n%s\nwant\n%s", sys.files[p.plistPath], minimalPlist)
	}
	if want := "[ocwarden install] plist rendered + lint-clean: /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	i, sys, out = newRecordingInstaller(true)
	if err := i.writePlist(p); err != nil {
		t.Fatalf("writePlist dry run: %v", err)
	}
	if len(sys.calls) != 0 {
		t.Errorf("a dry run wrote %v", sys.calls)
	}
	if want := "[ocwarden install] DRYRUN would: mkdir -p /Users/eva/Library/LaunchAgents; render plist -> /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist (XML-lint clean)\n"; out.String() != want {
		t.Errorf("log = %q, want %q", out.String(), want)
	}

	malformed := installFixture()
	malformed.ocBase = "https://oc.example.com/<unescaped"
	for _, dryRun := range []bool{false, true} {
		i, sys, _ = newRecordingInstaller(dryRun)
		err := i.writePlist(malformed)
		if err == nil || !strings.HasPrefix(err.Error(), "rendered plist is not well-formed XML: ") {
			t.Errorf("dryRun=%v: err = %v, want a well-formedness refusal", dryRun, err)
		}
		if len(sys.calls) != 0 {
			t.Errorf("dryRun=%v: a malformed render still did %v", dryRun, sys.calls)
		}
	}

	i, sys, _ = newRecordingInstaller(false)
	sys.fail["run plutil -lint /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist"] = errors.New("exit status 1")
	err := i.writePlist(p)
	wantErr := "rendered plist failed plutil -lint /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist: exit status 1"
	if err == nil || err.Error() != wantErr {
		t.Errorf("err = %v, want %q", err, wantErr)
	}

	i, sys, _ = newRecordingInstaller(false)
	sys.fail["mkdir /Users/eva/Library/LaunchAgents 755"] = errors.New("permission denied")
	err = i.writePlist(p)
	if err == nil || err.Error() != "mkdir LaunchAgents /Users/eva/Library/LaunchAgents: permission denied" {
		t.Errorf("err = %v, want the mkdir failure", err)
	}
}

func TestEnsureLogDir(t *testing.T) {
	t.Skip("a one-line mkdir wrapper; its live mkdir and its dry-run silence are asserted in TestRunInstall")
}

func TestBootoutUntilGone(t *testing.T) {
	target := "gui/501/com.officraft.ocwarden"
	notLoaded := errors.New("Could not find service in domain")

	t.Run("an unloaded label is confirmed on the first probe", func(t *testing.T) {
		sys := newSysRecorder()
		sys.run = func(argv string, _ int) (string, error) {
			if argv == "launchctl print "+target {
				return "", notLoaded
			}
			return "", nil
		}
		if !bootoutUntilGone(sys.ops(), target) {
			t.Error("bootoutUntilGone = false, want true")
		}
		want := []string{"run launchctl bootout " + target, "run launchctl print " + target}
		if !reflect.DeepEqual(sys.calls, want) {
			t.Errorf("calls = %v, want %v", sys.calls, want)
		}
	})

	t.Run("a bootout error is tolerated", func(t *testing.T) {
		sys := newSysRecorder()
		sys.run = func(argv string, _ int) (string, error) { return "", notLoaded }
		if !bootoutUntilGone(sys.ops(), target) {
			t.Error("bootoutUntilGone = false, want true")
		}
	})

	t.Run("the poll waits out an async deregistration", func(t *testing.T) {
		sys := newSysRecorder()
		sys.run = func(argv string, nth int) (string, error) {
			if argv == "launchctl print "+target && nth < 2 {
				return "state = running", nil
			}
			if argv == "launchctl print "+target {
				return "", notLoaded
			}
			return "", nil
		}
		if !bootoutUntilGone(sys.ops(), target) {
			t.Error("bootoutUntilGone = false, want true")
		}
		if n := sys.count("run launchctl print " + target); n != 3 {
			t.Errorf("%d print probes, want 3", n)
		}
		if n := sys.count("sleep 200ms"); n != 2 {
			t.Errorf("%d sleeps, want 2", n)
		}
	})

	t.Run("a lingering label times out", func(t *testing.T) {
		sys := newSysRecorder()
		sys.run = func(string, int) (string, error) { return "state = running", nil }
		if bootoutUntilGone(sys.ops(), target) {
			t.Error("bootoutUntilGone = true, want false")
		}
		if n := sys.count("run launchctl print " + target); n != 25 {
			t.Errorf("%d print probes, want 25", n)
		}
		if n := sys.count("sleep 200ms"); n != 25 {
			t.Errorf("%d sleeps, want 25", n)
		}
	})
}

func TestRegisteredUntilFound(t *testing.T) {
	target := "gui/501/com.officraft.ocwarden"
	unknown := errors.New("Could not find service in domain")

	sys := newSysRecorder()
	if err := registeredUntilFound(sys.ops(), target); err != nil {
		t.Errorf("registeredUntilFound = %v, want nil", err)
	}
	if want := []string{"run launchctl print " + target}; !reflect.DeepEqual(sys.calls, want) {
		t.Errorf("calls = %v, want %v", sys.calls, want)
	}

	lagging := newSysRecorder()
	lagging.run = func(_ string, nth int) (string, error) {
		if nth < 2 {
			return "", unknown
		}
		return "state = running", nil
	}
	if err := registeredUntilFound(lagging.ops(), target); err != nil {
		t.Errorf("registeredUntilFound = %v, want nil", err)
	}
	if n := lagging.count("run launchctl print " + target); n != 3 {
		t.Errorf("%d print probes, want 3", n)
	}
	if n := lagging.count("sleep 200ms"); n != 2 {
		t.Errorf("%d sleeps, want 2", n)
	}

	never := newSysRecorder()
	never.run = func(string, int) (string, error) { return "", unknown }
	err := registeredUntilFound(never.ops(), target)
	if err == nil || err.Error() != "Could not find service in domain" {
		t.Errorf("registeredUntilFound = %v, want the last probe's error", err)
	}
	if n := never.count("run launchctl print " + target); n != 25 {
		t.Errorf("%d print probes, want 25", n)
	}
}

func TestLaunchctlReinstall(t *testing.T) {
	p := installFixture()
	target := "gui/501/com.officraft.ocwarden"
	notLoaded := errors.New("Could not find service in domain")

	// gone then registered: the print probe answers "unloaded" once (bootout
	// confirmed) and "loaded" from then on (bootstrap confirmed).
	settling := func(argv string, nth int) (string, error) {
		if argv == "launchctl print "+target && nth == 0 {
			return "", notLoaded
		}
		return "state = running", nil
	}

	t.Run("bootout, bootstrap, kickstart under the exact label", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.run = settling
		if err := i.launchctlReinstall(p); err != nil {
			t.Fatalf("launchctlReinstall: %v", err)
		}
		want := []string{
			"run launchctl bootout " + target,
			"run launchctl print " + target,
			"run launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
			"run launchctl print " + target,
			"run launchctl kickstart -k " + target,
		}
		if !reflect.DeepEqual(sys.calls, want) {
			t.Errorf("calls =\n%v\nwant\n%v", sys.calls, want)
		}
		if want := "[ocwarden install] bootstrapped + kickstarted com.officraft.ocwarden (exact label; python warden untouched)\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("a namespaced instance acts on its own label only", func(t *testing.T) {
		ns := installFixture()
		ns.namespace = "lab"
		ns.label = "com.officraft.ocwarden.lab"
		ns.plistPath = "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.lab.plist"
		nsTarget := "gui/501/com.officraft.ocwarden.lab"
		i, sys, _ := newRecordingInstaller(false)
		sys.run = func(argv string, nth int) (string, error) {
			if argv == "launchctl print "+nsTarget && nth == 0 {
				return "", notLoaded
			}
			return "state = running", nil
		}
		if err := i.launchctlReinstall(ns); err != nil {
			t.Fatalf("launchctlReinstall: %v", err)
		}
		want := []string{
			"run launchctl bootout " + nsTarget,
			"run launchctl print " + nsTarget,
			"run launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.lab.plist",
			"run launchctl print " + nsTarget,
			"run launchctl kickstart -k " + nsTarget,
		}
		if !reflect.DeepEqual(sys.calls, want) {
			t.Errorf("calls =\n%v\nwant\n%v", sys.calls, want)
		}
	})

	t.Run("a lingering old registration is bootstrapped over anyway", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.run = func(string, int) (string, error) { return "state = running", nil }
		if err := i.launchctlReinstall(p); err != nil {
			t.Fatalf("launchctlReinstall: %v", err)
		}
		want := "[ocwarden install] WARN: com.officraft.ocwarden still registered ~5s after bootout; bootstrapping anyway\n" +
			"[ocwarden install] bootstrapped + kickstarted com.officraft.ocwarden (exact label; python warden untouched)\n"
		if out.String() != want {
			t.Errorf("log =\n%s\nwant\n%s", out.String(), want)
		}
		if n := sys.count("run launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist"); n != 1 {
			t.Errorf("%d bootstraps, want 1", n)
		}
	})

	t.Run("a failed bootstrap stops before kickstart", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.run = settling
		sys.fail["run launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist"] =
			errors.New("Bootstrap failed: 5: Input/output error")
		err := i.launchctlReinstall(p)
		wantErr := "launchctl bootstrap failed for /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist: " +
			"Bootstrap failed: 5: Input/output error"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if n := sys.count("run launchctl kickstart -k " + target); n != 0 {
			t.Errorf("kickstart ran %d times after a failed bootstrap, want 0", n)
		}
	})

	t.Run("a registered label that will not kickstart", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.run = settling
		sys.fail["run launchctl kickstart -k "+target] = errors.New("exit status 5")
		err := i.launchctlReinstall(p)
		wantErr := "launchctl kickstart failed for " + target + ": exit status 5"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	})

	t.Run("a bootstrap that exited 0 and registered nothing", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.run = func(argv string, _ int) (string, error) {
			if argv == "launchctl print "+target {
				return "", notLoaded
			}
			return "", nil
		}
		sys.fail["run launchctl kickstart -k "+target] = errors.New("Could not find service \"com.officraft.ocwarden\" in domain")
		err := i.launchctlReinstall(p)
		wantErr := "launchctl bootstrap exited 0 but registered nothing — launchd does not know " + target +
			", so the job was never loaded; the plist to look at is /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist: " +
			"Could not find service in domain (the next verb then reported: Could not find service \"com.officraft.ocwarden\" in domain)"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err =\n%v\nwant\n%s", err, wantErr)
		}
		want := "[ocwarden install] WARN: launchd still does not know com.officraft.ocwarden ~5s after bootstrap exited 0; kickstarting anyway\n"
		if out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("a dry run narrates every launchctl verb and runs none", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		if err := i.launchctlReinstall(p); err != nil {
			t.Fatalf("launchctlReinstall dry run: %v", err)
		}
		if len(sys.calls) != 0 {
			t.Errorf("a dry run ran %v", sys.calls)
		}
		want := "[ocwarden install] DRYRUN would run: launchctl bootout " + target + "  (tolerate not-loaded)\n" +
			"[ocwarden install] DRYRUN would: poll `launchctl print " + target + "` until the label is gone (bootout is async; bounded ~5s)\n" +
			"[ocwarden install] DRYRUN would run: launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist\n" +
			"[ocwarden install] DRYRUN would: poll `launchctl print " + target + "` until the label is registered (bootstrap can exit 0 and register nothing; bounded ~5s, non-fatal)\n" +
			"[ocwarden install] DRYRUN would run: launchctl kickstart -k " + target + "\n"
		if out.String() != want {
			t.Errorf("log =\n%s\nwant\n%s", out.String(), want)
		}
	})
}

func TestVerify(t *testing.T) {
	p := installFixture()
	target := "gui/501/com.officraft.ocwarden"
	notLoaded := errors.New("Could not find service in domain")
	header := "[ocwarden install] verifying com.officraft.ocwarden is alive AND STABLE...\n"

	t.Run("a pid that holds across the settle window", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.run = func(string, int) (string, error) { return "\tstate = running\n\tpid = 4242\n", nil }
		if err := i.verify(p); err != nil {
			t.Fatalf("verify: %v", err)
		}
		if n := sys.count("run launchctl print " + target); n != 7 {
			t.Errorf("%d print probes, want 7 (one to acquire, six to settle)", n)
		}
		if n := sys.count("sleep 1s"); n != 6 {
			t.Errorf("%d settle sleeps, want 6", n)
		}
		want := header + "[ocwarden install] SUCCESS: com.officraft.ocwarden is running and STABLE (pid=4242 held >=6s). " +
			"Logs: /Users/eva/.officraft/warden/log/ocwarden.{out,err}.log\n"
		if out.String() != want {
			t.Errorf("log =\n%s\nwant\n%s", out.String(), want)
		}
	})

	t.Run("a job that never reports a pid", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.run = func(string, int) (string, error) { return "", notLoaded }
		err := i.verify(p)
		if err == nil || err.Error() != "com.officraft.ocwarden did not report a live PID within 30s" {
			t.Errorf("err = %v, want the no-pid refusal", err)
		}
		if n := sys.count("run launchctl print " + target); n != 30 {
			t.Errorf("%d print probes, want 30", n)
		}
		if n := sys.count("sleep 1s"); n != 30 {
			t.Errorf("%d sleeps, want 30", n)
		}
		if out.String() != header {
			t.Errorf("log = %q, want %q", out.String(), header)
		}
	})

	t.Run("a warden respawned under a different pid is crash-looping", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.run = func(_ string, nth int) (string, error) {
			if nth < 3 {
				return "\tpid = 4242\n", nil
			}
			return "\tpid = 4311\n", nil
		}
		err := i.verify(p)
		wantErr := "com.officraft.ocwarden is CRASH-LOOPING — pid did not hold across the settle window " +
			"(first=4242, then=4311); likely bad token / server unreachable / auth reject"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	})

	t.Run("a warden that disappears during the settle window", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.run = func(_ string, nth int) (string, error) {
			if nth == 0 {
				return "\tpid = 4242\n", nil
			}
			return "", notLoaded
		}
		err := i.verify(p)
		wantErr := "com.officraft.ocwarden is CRASH-LOOPING — pid did not hold across the settle window " +
			"(first=4242, then=<none>); likely bad token / server unreachable / auth reject"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
	})

	t.Run("a dry run verifies nothing", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		if err := i.verify(p); err != nil {
			t.Fatalf("verify dry run: %v", err)
		}
		if len(sys.calls) != 0 {
			t.Errorf("a dry run ran %v", sys.calls)
		}
		if want := "[ocwarden install] DRYRUN: skipping live verification (no machine state changed)\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})
}

func TestWardenPID(t *testing.T) {
	target := "gui/501/com.officraft.ocwarden"
	cases := []struct {
		name string
		out  string
		err  error
		want string
	}{
		{"a loaded job", "com.officraft.ocwarden = {\n\tactive count = 1\n\tpid = 4242\n\tstate = running\n}\n", nil, "4242"},
		{"the first pid line wins", "\tpid = 4242\n\tpid = 9999\n", nil, "4242"},
		{"an unloaded label", "", errors.New("Could not find service in domain"), ""},
		{"output that carries a pid but exited non-zero", "\tpid = 4242\n", errors.New("exit status 1"), ""},
		{"a job with no pid line", "com.officraft.ocwarden = {\n\tstate = not running\n}\n", nil, ""},
		{"a non-numeric pid", "\tpid = none\n", nil, ""},
		{"a pid that is not at the start of a line", "\tlast exit pid = 4242\n", nil, ""},
	}
	for _, c := range cases {
		i, sys, _ := newRecordingInstaller(false)
		sys.run = func(string, int) (string, error) { return c.out, c.err }
		if got := i.wardenPID(target); got != c.want {
			t.Errorf("%s: wardenPID = %q, want %q", c.name, got, c.want)
		}
		if want := []string{"run launchctl print " + target}; !reflect.DeepEqual(sys.calls, want) {
			t.Errorf("%s: calls = %v, want %v", c.name, sys.calls, want)
		}
	}
}

func TestOrNone(t *testing.T) {
	t.Skip("a two-line placeholder for the empty string; both branches are asserted in TestVerify's crash-loop messages")
}

func TestRunInstall(t *testing.T) {
	target := "gui/501/com.officraft.ocwarden"

	t.Run("a dry run reads twice and mutates nothing", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		if err := i.runInstall(installFixture()); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
		want := []string{
			"read /Users/eva/.officraft/warden/exec-warden.tok",
			"read /Users/eva/.officraft/warden/officraft",
		}
		if !reflect.DeepEqual(sys.calls, want) {
			t.Errorf("calls =\n%v\nwant only the two non-mutating probes\n%v", sys.calls, want)
		}
		wantLog := "[ocwarden install] resolved: ROOT=/Users/eva/.officraft HOME=/Users/eva OC_BASE=https://oc.example.com OC_ID=<derive-from-token-sub>\n" +
			"[ocwarden install] targets:  TOKFILE=/Users/eva/.officraft/warden/exec-warden.tok PLIST=/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist BIN=/Users/eva/.officraft/warden/ocwarden SRC=/tmp/clone/ocwarden\n" +
			"[ocwarden install] ocagent:  DOWNLOAD https://oc.example.com/api/agent/binary -> BIN=/Users/eva/.officraft/warden/ocagent (home sibling; runtime-discovered, not in plist)\n" +
			"[ocwarden install] DRY-RUN mode: no file writes / no launchctl / no verification.\n" +
			"[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; copy /tmp/clone/ocwarden -> a 0755 temp then atomic rename -> /Users/eva/.officraft/warden/ocwarden\n" +
			"[ocwarden install] DRYRUN would: copy fixed identity anchor /tmp/clone/officraft -> /Users/eva/.officraft/warden/officraft only if absent\n" +
			"[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; download ocagent from https://oc.example.com/api/agent/binary -> verify-exec -> atomic rename -> /Users/eva/.officraft/warden/ocagent\n" +
			"[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden; write <token> to a 0600 temp then atomic rename -> /Users/eva/.officraft/warden/exec-warden.tok\n" +
			"[ocwarden install] DRYRUN would: mkdir -p /Users/eva/.officraft/warden/log\n" +
			"[ocwarden install] DRYRUN would: mkdir -p /Users/eva/Library/LaunchAgents; render plist -> /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist (XML-lint clean)\n" +
			"[ocwarden install] DRYRUN would run: launchctl bootout " + target + "  (tolerate not-loaded)\n" +
			"[ocwarden install] DRYRUN would: poll `launchctl print " + target + "` until the label is gone (bootout is async; bounded ~5s)\n" +
			"[ocwarden install] DRYRUN would run: launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist\n" +
			"[ocwarden install] DRYRUN would: poll `launchctl print " + target + "` until the label is registered (bootstrap can exit 0 and register nothing; bounded ~5s, non-fatal)\n" +
			"[ocwarden install] DRYRUN would run: launchctl kickstart -k " + target + "\n" +
			"[ocwarden install] DRYRUN: skipping live verification (no machine state changed)\n" +
			"[ocwarden install] DRYRUN complete — no machine state changed.\n"
		if out.String() != wantLog {
			t.Errorf("log =\n%s\nwant\n%s", out.String(), wantLog)
		}
	})

	t.Run("the six steps leave the machine installed", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		sys.files["/tmp/clone/ocwarden"] = "OCWARDEN-BYTES"
		sys.files["/tmp/clone/officraft"] = "ANCHOR-BYTES"
		sys.run = func(argv string, nth int) (string, error) {
			if argv == "launchctl print "+target && nth == 0 {
				return "", errors.New("Could not find service in domain")
			}
			return "\tpid = 4242\n", nil
		}
		i.agentGet = func(string) (int, []byte, error) { return 200, []byte("OCAGENT-BYTES"), nil }
		i.agentProbe = func(string) error { return nil }
		i.resolveClaude = func() (string, string) { return "/opt/claude", "" }
		i.resolveCodex = func() (string, string) { return "/opt/codex", "/opt/x&y:/bin" }

		if err := i.runInstall(installFixture()); err != nil {
			t.Fatalf("runInstall: %v", err)
		}

		wantFiles := map[string]string{
			"/tmp/clone/ocwarden":                                          "OCWARDEN-BYTES",
			"/tmp/clone/officraft":                                         "ANCHOR-BYTES",
			"/Users/eva/.officraft/warden/ocwarden":                        "OCWARDEN-BYTES",
			"/Users/eva/.officraft/warden/officraft":                       "ANCHOR-BYTES",
			"/Users/eva/.officraft/warden/ocagent":                         "OCAGENT-BYTES",
			"/Users/eva/.officraft/warden/exec-warden.tok":                 jwtWardenOne,
			"/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist": stampedPlist,
		}
		if !reflect.DeepEqual(sys.files, wantFiles) {
			for path, body := range sys.files {
				if wantFiles[path] != body {
					t.Errorf("%s holds\n%s\nwant\n%s", path, body, wantFiles[path])
				}
			}
			if len(sys.files) != len(wantFiles) {
				t.Errorf("installed paths = %d, want %d", len(sys.files), len(wantFiles))
			}
		}
		wantPerms := map[string]os.FileMode{
			"/Users/eva/.officraft/warden/ocwarden":                        0o755,
			"/Users/eva/.officraft/warden/officraft":                       0o755,
			"/Users/eva/.officraft/warden/ocagent":                         0o755,
			"/Users/eva/.officraft/warden/exec-warden.tok":                 0o600,
			"/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist": 0o644,
		}
		if !reflect.DeepEqual(sys.perms, wantPerms) {
			t.Errorf("perms = %v, want %v", sys.perms, wantPerms)
		}
		wantRuns := []string{
			"run plutil -lint /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
			"run launchctl bootout " + target,
			"run launchctl print " + target,
			"run launchctl bootstrap gui/501 /Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
			"run launchctl print " + target,
			"run launchctl kickstart -k " + target,
			"run launchctl print " + target,
			"run launchctl print " + target,
			"run launchctl print " + target,
			"run launchctl print " + target,
			"run launchctl print " + target,
			"run launchctl print " + target,
			"run launchctl print " + target,
		}
		if !reflect.DeepEqual(sys.runCalls(), wantRuns) {
			t.Errorf("subprocesses =\n%v\nwant\n%v", sys.runCalls(), wantRuns)
		}
		wantHead := "[ocwarden install] resolved: ROOT=/Users/eva/.officraft HOME=/Users/eva OC_BASE=https://oc.example.com OC_ID=<derive-from-token-sub>\n" +
			"[ocwarden install] targets:  TOKFILE=/Users/eva/.officraft/warden/exec-warden.tok PLIST=/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist BIN=/Users/eva/.officraft/warden/ocwarden SRC=/tmp/clone/ocwarden\n" +
			"[ocwarden install] ocagent:  DOWNLOAD https://oc.example.com/api/agent/binary -> BIN=/Users/eva/.officraft/warden/ocagent (home sibling; runtime-discovered, not in plist)\n" +
			"[ocwarden install] claude:   /opt/claude (stamped OC_CLAUDE_BIN into the plist)\n" +
			"[ocwarden install] codex:    /opt/codex (stamped OC_CODEX_BIN into the plist)\n"
		if !strings.HasPrefix(out.String(), wantHead) {
			t.Errorf("log =\n%s\nwant it to open with\n%s", out.String(), wantHead)
		}
	})

	t.Run("a claude that needs the installer PATH is announced", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		i.resolveClaude = func() (string, string) { return "/Users/eva/.asdf/shims/claude", "/Users/eva/.asdf/shims:/usr/bin" }
		if err := i.runInstall(installFixture()); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
		_ = sys
		want := "[ocwarden install] claude:   /Users/eva/.asdf/shims/claude (stamped OC_CLAUDE_BIN + installer PATH into the plist — shim needs it)\n"
		if !strings.Contains(out.String(), want) {
			t.Errorf("log =\n%s\nwant it to contain\n%s", out.String(), want)
		}
	})

	t.Run("no provider installs nothing", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		i.resolveClaude = func() (string, string) { return "", "" }
		i.resolveCodex = func() (string, string) { return "", "" }
		err := i.runInstall(installFixture())
		wantErr := "runtime_bin_unresolved: install claude or codex, or set OC_CLAUDE_BIN/OC_CODEX_BIN"
		if err == nil || err.Error() != wantErr {
			t.Errorf("err = %v, want %q", err, wantErr)
		}
		if len(sys.calls) != 0 {
			t.Errorf("a refused install still did %v", sys.calls)
		}
		want := "[ocwarden install] FATAL: neither Claude Code nor Codex CLI is available — NOTHING was installed\n" +
			"[ocwarden install] FATAL: runtime_bin_unresolved: install a provider or set OC_CLAUDE_BIN/OC_CODEX_BIN, then re-run safely\n"
		if !strings.HasSuffix(out.String(), want) {
			t.Errorf("log =\n%s\nwant it to end with\n%s", out.String(), want)
		}
	})

	t.Run("a foreign warden is refused before anything is written", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.files["/Users/eva/.officraft/warden/exec-warden.tok"] = jwtWardenTwo
		err := i.runInstall(installFixture())
		if err == nil || !strings.HasPrefix(err.Error(), "refusing: a warden for machine warden-2") {
			t.Errorf("err = %v, want the one-warden-per-machine refusal", err)
		}
		if want := []string{"read /Users/eva/.officraft/warden/exec-warden.tok"}; !reflect.DeepEqual(sys.calls, want) {
			t.Errorf("calls = %v, want %v", sys.calls, want)
		}
	})

	t.Run("a failing step stops the sequence before launchctl", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.files["/tmp/clone/ocwarden"] = "OCWARDEN-BYTES"
		sys.files["/tmp/clone/officraft"] = "ANCHOR-BYTES"
		i.agentGet = func(string) (int, []byte, error) { return 200, []byte("OCAGENT-BYTES"), nil }
		i.agentProbe = func(string) error { return nil }
		sys.fail["mkdir /Users/eva/.officraft/warden 700"] = errors.New("read-only file system")
		err := i.runInstall(installFixture())
		if err == nil || err.Error() != "mkdir tokfile dir /Users/eva/.officraft/warden: read-only file system" {
			t.Errorf("err = %v, want the tokfile mkdir failure", err)
		}
		if runs := sys.runCalls(); len(runs) != 0 {
			t.Errorf("a failed install still ran %v", runs)
		}
		if _, wrote := sys.files["/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist"]; wrote {
			t.Error("a failed install still wrote the plist")
		}
	})
}

func TestInstallCmd(t *testing.T) {
	t.Skip("the entry point builds its effects from newHostSeam(), which this package binds to realHostSeam — calling it from a test binary is a deliberate os.Exit(1) (TestRealHostSeam) that would take the whole test process with it")
}
