package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolvedTempDir is a temp directory with every symlink already resolved, so a
// test can compose expected paths by hand on a host whose /tmp or /var is a link.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// agentWorkdir creates <home>/member-alice and returns the config that names it
// plus the workdir path itself.
func agentWorkdir(t *testing.T) (Config, string) {
	t.Helper()
	home := resolvedTempDir(t)
	root := filepath.Join(home, "member-alice")
	mkdirAll(t, root)
	return Config{Home: home, ID: "Member-Alice"}, root
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFileAt(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlinkAt(t *testing.T, target, link string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}

func readFileAt(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCleanRoot(t *testing.T) {
	t.Run("the workdir is the agent home joined with its lowercased id", func(t *testing.T) {
		home := resolvedTempDir(t)
		got, err := cleanRoot(Config{Home: home, ID: "Member-ALICE"})
		if err != nil || got != filepath.Join(home, "member-alice") {
			t.Fatalf("cleanRoot = (%q, %v), want (%q, nil)",
				got, err, filepath.Join(home, "member-alice"))
		}
	})

	t.Run("a workdir that does not exist yet is still named", func(t *testing.T) {
		got, err := cleanRoot(Config{Home: "/no/such/home", ID: "member-alice"})
		if err != nil || got != "/no/such/home/member-alice" {
			t.Fatalf("cleanRoot = (%q, %v), want (\"/no/such/home/member-alice\", nil)", got, err)
		}
	})

	t.Run("a symlinked home is resolved once so nothing later looks like an escape", func(t *testing.T) {
		base := resolvedTempDir(t)
		real := filepath.Join(base, "real")
		mkdirAll(t, filepath.Join(real, "member-alice"))
		symlinkAt(t, real, filepath.Join(base, "link"))

		got, err := cleanRoot(Config{Home: filepath.Join(base, "link"), ID: "member-alice"})
		if err != nil || got != filepath.Join(real, "member-alice") {
			t.Fatalf("cleanRoot = (%q, %v), want (%q, nil)",
				got, err, filepath.Join(real, "member-alice"))
		}
	})

	t.Run("no id refuses rather than falling back to a shared tree", func(t *testing.T) {
		for _, id := range []string{"", "   "} {
			got, err := cleanRoot(Config{Home: "/home", ID: id})
			if got != "" || err == nil ||
				err.Error() != "no agent id (OC_ID / OC_TOKEN): cannot tell which workdir is mine" {
				t.Fatalf("cleanRoot(ID=%q) = (%q, %v), want the no-id refusal", id, got, err)
			}
		}
	})

	t.Run("no home refuses", func(t *testing.T) {
		for _, home := range []string{"", "  "} {
			got, err := cleanRoot(Config{Home: home, ID: "member-alice"})
			if got != "" || err == nil ||
				err.Error() != "no agent home (OC_AGENT_HOME): cannot tell which workdir is mine" {
				t.Fatalf("cleanRoot(Home=%q) = (%q, %v), want the no-home refusal", home, got, err)
			}
		}
	})
}

func TestCanonicalise(t *testing.T) {
	base := resolvedTempDir(t)
	real := filepath.Join(base, "real")
	mkdirAll(t, real)
	writeFileAt(t, filepath.Join(real, "kept.txt"), "x")
	symlinkAt(t, real, filepath.Join(base, "link"))
	symlinkAt(t, "/nowhere/nothing", filepath.Join(real, "dangling"))

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"an existing path comes back resolved", filepath.Join(base, "link", "kept.txt"),
			filepath.Join(real, "kept.txt")},
		{"a real directory is unchanged", real, real},
		{"a missing leaf is re-attached to its resolved parent",
			filepath.Join(base, "link", "gone.txt"), filepath.Join(real, "gone.txt")},
		{"several missing components are re-attached in order",
			filepath.Join(base, "link", "a", "b", "c.txt"), filepath.Join(real, "a", "b", "c.txt")},
		{"a dangling symlink resolves to itself, not to what it points at",
			filepath.Join(real, "dangling"), filepath.Join(real, "dangling")},
		{"a path below a dangling symlink keeps the link in it",
			filepath.Join(real, "dangling", "x"), filepath.Join(real, "dangling", "x")},
		{"a path with no existing ancestor comes back cleaned but unresolved",
			"/no-such-root-xyz/a/b", "/no-such-root-xyz/a/b"},
		{"dot and dot-dot are cleaned away first",
			filepath.Join(base, "link", ".", "a", "..", "b"), filepath.Join(real, "b")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalise(tc.in); got != tc.want {
				t.Fatalf("canonicalise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsUnder(t *testing.T) {
	cases := []struct {
		name       string
		root, path string
		want       bool
	}{
		{"a child is under", "/a", "/a/b", true},
		{"a grandchild is under", "/a", "/a/b/c", true},
		{"the root itself is not under", "/a", "/a", false},
		{"a sibling is not under", "/a", "/b", false},
		{"a prefix-sharing sibling is not under", "/a", "/ab", false},
		{"the parent is not under", "/a/b", "/a", false},
		{"a relative path against an absolute root is not under", "/a", "b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUnder(tc.root, tc.path); got != tc.want {
				t.Fatalf("isUnder(%q, %q) = %v, want %v", tc.root, tc.path, got, tc.want)
			}
		})
	}
}

func TestInsideRoot(t *testing.T) {
	const root = "/home/member-alice"
	cases := []struct {
		name string
		path string
		arg  string
		want string
	}{
		{"a file in the workdir is accepted", root + "/tmp/x.txt", "tmp/x.txt", ""},
		{"a deep file in the workdir is accepted", root + "/a/b/c/x.txt", "a/b/c/x.txt", ""},
		{"a name that merely starts with trash is accepted", root + "/trashcan/x", "trashcan/x", ""},
		{"the workdir itself is refused by name", root, ".",
			"that is my workdir itself, not something in it: ."},
		{"the parent is refused as outside", "/home", "..", "outside my workdir: .."},
		{"a sibling tree is refused as outside", "/home/member-bob/x", "../member-bob/x",
			"outside my workdir: ../member-bob/x"},
		{"a relative path cannot be compared and is refused as outside", "tmp/x", "tmp/x",
			"outside my workdir: tmp/x"},
		{"the quarantine directory itself is refused", root + "/trash", "trash",
			"that is already in trash/: trash"},
		{"something already parked in quarantine is refused", root + "/trash/tmp/x.txt",
			"trash/tmp/x.txt", "that is already in trash/: trash/tmp/x.txt"},
		{"quarantine spelled in a different case is still quarantine", root + "/TRASH/tmp/x.txt",
			"TRASH/tmp/x.txt", "that is already in trash/: TRASH/tmp/x.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := insideRoot(root, tc.path, tc.arg)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("insideRoot(%q, %q) = %q, want %q", root, tc.path, got, tc.want)
			}
		})
	}
}

func TestResolveInsideRoot(t *testing.T) {
	t.Run("an ordinary in-tree file resolves to itself", func(t *testing.T) {
		_, root := agentWorkdir(t)
		writeFileAt(t, filepath.Join(root, "tmp", "x.txt"), "x")
		got, err := resolveInsideRoot(root, filepath.Join(root, "tmp", "x.txt"))
		if err != nil || got != filepath.Join(root, "tmp", "x.txt") {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the same path", got, err)
		}
	})

	t.Run("a target that does not exist is fine, so a re-run is idempotent", func(t *testing.T) {
		_, root := agentWorkdir(t)
		got, err := resolveInsideRoot(root, filepath.Join(root, "tmp", "gone.txt"))
		if err != nil || got != filepath.Join(root, "tmp", "gone.txt") {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the same path", got, err)
		}
	})

	t.Run("a relative argument is resolved against the working directory", func(t *testing.T) {
		_, root := agentWorkdir(t)
		writeFileAt(t, filepath.Join(root, "tmp", "x.txt"), "x")
		t.Chdir(root)
		got, err := resolveInsideRoot(root, "tmp/x.txt")
		if err != nil || got != filepath.Join(root, "tmp", "x.txt") {
			t.Fatalf("resolveInsideRoot = (%q, %v), want %q", got, err, filepath.Join(root, "tmp", "x.txt"))
		}
	})

	t.Run("a symlink leaf pointing inside the tree stays the link, not its pointee", func(t *testing.T) {
		_, root := agentWorkdir(t)
		writeFileAt(t, filepath.Join(root, "d.txt"), "x")
		symlinkAt(t, filepath.Join(root, "d.txt"), filepath.Join(root, "inlink"))
		got, err := resolveInsideRoot(root, filepath.Join(root, "inlink"))
		if err != nil || got != filepath.Join(root, "inlink") {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the link path itself", got, err)
		}
	})

	t.Run("a symlink leaf pointing out of the tree is refused", func(t *testing.T) {
		_, root := agentWorkdir(t)
		outside := filepath.Join(resolvedTempDir(t), "elsewhere.txt")
		writeFileAt(t, outside, "x")
		symlinkAt(t, outside, filepath.Join(root, "outlink"))
		arg := filepath.Join(root, "outlink")
		got, err := resolveInsideRoot(root, arg)
		if got != "" || err == nil || err.Error() != "outside my workdir: "+arg {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the outside refusal", got, err)
		}
	})

	t.Run("a dangling symlink leaf is the debris this command exists to clear", func(t *testing.T) {
		_, root := agentWorkdir(t)
		symlinkAt(t, filepath.Join(root, "never-existed"), filepath.Join(root, "dangling"))
		got, err := resolveInsideRoot(root, filepath.Join(root, "dangling"))
		if err != nil || got != filepath.Join(root, "dangling") {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the link path itself", got, err)
		}
	})

	t.Run("a symlink dangling at a path outside the tree is still just a link in the tree", func(t *testing.T) {
		_, root := agentWorkdir(t)
		symlinkAt(t, "/no-such-root-xyz/gone.txt", filepath.Join(root, "dangling-out"))
		got, err := resolveInsideRoot(root, filepath.Join(root, "dangling-out"))
		if err != nil || got != filepath.Join(root, "dangling-out") {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the link path itself", got, err)
		}
	})

	t.Run("an intermediate symlinked directory pointing out of the tree is refused", func(t *testing.T) {
		_, root := agentWorkdir(t)
		outside := filepath.Join(resolvedTempDir(t), "elsewhere")
		writeFileAt(t, filepath.Join(outside, "x.txt"), "x")
		symlinkAt(t, outside, filepath.Join(root, "bridge"))
		arg := filepath.Join(root, "bridge", "x.txt")
		got, err := resolveInsideRoot(root, arg)
		if got != "" || err == nil || err.Error() != "outside my workdir: "+arg {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the outside refusal", got, err)
		}
	})

	t.Run("the workdir itself is refused", func(t *testing.T) {
		_, root := agentWorkdir(t)
		got, err := resolveInsideRoot(root, root)
		if got != "" || err == nil ||
			err.Error() != "that is my workdir itself, not something in it: "+root {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the workdir refusal", got, err)
		}
	})

	t.Run("something already in quarantine is refused", func(t *testing.T) {
		_, root := agentWorkdir(t)
		writeFileAt(t, filepath.Join(root, "trash", "tmp", "x.txt"), "x")
		arg := filepath.Join(root, "trash", "tmp", "x.txt")
		got, err := resolveInsideRoot(root, arg)
		if got != "" || err == nil || err.Error() != "that is already in trash/: "+arg {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the already-quarantined refusal", got, err)
		}
	})

	t.Run("a path outside the workdir is refused", func(t *testing.T) {
		_, root := agentWorkdir(t)
		arg := filepath.Join(resolvedTempDir(t), "elsewhere.txt")
		writeFileAt(t, arg, "x")
		got, err := resolveInsideRoot(root, arg)
		if got != "" || err == nil || err.Error() != "outside my workdir: "+arg {
			t.Fatalf("resolveInsideRoot = (%q, %v), want the outside refusal", got, err)
		}
	})

	t.Run("an empty argument is refused", func(t *testing.T) {
		_, root := agentWorkdir(t)
		for _, arg := range []string{"", "   "} {
			got, err := resolveInsideRoot(root, arg)
			if got != "" || err == nil || err.Error() != "empty path" {
				t.Fatalf("resolveInsideRoot(%q) = (%q, %v), want the empty-path refusal", arg, got, err)
			}
		}
	})
}

func TestQuarantineDest(t *testing.T) {
	t.Run("the path relative to the workdir is preserved under trash", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "notes", "x.txt")
		writeFileAt(t, target, "x")
		got, err := quarantineDest(root, target)
		want := filepath.Join(root, "trash", "tmp", "notes", "x.txt")
		if err != nil || got != want {
			t.Fatalf("quarantineDest = (%q, %v), want (%q, nil)", got, err, want)
		}
		if !exists(t, filepath.Dir(want)) {
			t.Fatalf("%s was not created", filepath.Dir(want))
		}
		if exists(t, want) {
			t.Fatalf("%s exists — quarantineDest must pick a slot, not create the file", want)
		}
	})

	t.Run("a top-level target lands directly under trash", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "x.txt")
		writeFileAt(t, target, "x")
		got, err := quarantineDest(root, target)
		want := filepath.Join(root, "trash", "x.txt")
		if err != nil || got != want {
			t.Fatalf("quarantineDest = (%q, %v), want (%q, nil)", got, err, want)
		}
	})

	t.Run("an occupied slot is never overwritten: it becomes -2, then -3", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "x.txt")
		writeFileAt(t, target, "x")
		writeFileAt(t, filepath.Join(root, "trash", "tmp", "x.txt"), "first")
		got, err := quarantineDest(root, target)
		want := filepath.Join(root, "trash", "tmp", "x.txt-2")
		if err != nil || got != want {
			t.Fatalf("quarantineDest = (%q, %v), want (%q, nil)", got, err, want)
		}
		writeFileAt(t, want, "second")
		got, err = quarantineDest(root, target)
		want = filepath.Join(root, "trash", "tmp", "x.txt-3")
		if err != nil || got != want {
			t.Fatalf("quarantineDest = (%q, %v), want (%q, nil)", got, err, want)
		}
		if readFileAt(t, filepath.Join(root, "trash", "tmp", "x.txt")) != "first" {
			t.Fatal("the previously parked file was disturbed")
		}
	})

	t.Run("an occupied slot that is a symlink still counts as taken", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "x.txt")
		writeFileAt(t, target, "x")
		symlinkAt(t, "/nowhere/nothing", filepath.Join(root, "trash", "x.txt"))
		got, err := quarantineDest(root, target)
		want := filepath.Join(root, "trash", "x.txt-2")
		if err != nil || got != want {
			t.Fatalf("quarantineDest = (%q, %v), want (%q, nil)", got, err, want)
		}
	})

	t.Run("a slot that cannot be inspected is named rather than blamed on 1000 copies", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "x.txt")
		writeFileAt(t, target, "x")
		writeFileAt(t, filepath.Join(root, "trash", "tmp"), "i am a file, not a directory")
		got, err := quarantineDest(root, target)
		if got != "" || err == nil ||
			!strings.HasPrefix(err.Error(), "cannot inspect the trash/ slot tmp/x.txt: ") {
			t.Fatalf("quarantineDest = (%q, %v), want the cannot-inspect refusal", got, err)
		}
	})

	t.Run("a quarantine subdirectory that links out of trash is refused before anything is created", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "sub", "x.txt")
		writeFileAt(t, target, "x")
		mkdirAll(t, filepath.Join(root, "live"))
		symlinkAt(t, filepath.Join(root, "live"), filepath.Join(root, "trash", "tmp"))

		got, err := quarantineDest(root, target)
		if got != "" || err == nil || err.Error() != "the trash/ path does not stay in trash/" {
			t.Fatalf("quarantineDest = (%q, %v), want the does-not-stay refusal", got, err)
		}
		if exists(t, filepath.Join(root, "live", "sub")) {
			t.Fatal("a directory was created on the far side of the link")
		}
	})

	t.Run("a symlinked quarantine directory is refused even when it points inside the tree", func(t *testing.T) {
		_, root := agentWorkdir(t)
		target := filepath.Join(root, "x.txt")
		writeFileAt(t, target, "x")
		mkdirAll(t, filepath.Join(root, "elsewhere"))
		symlinkAt(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "trash"))

		got, err := quarantineDest(root, target)
		if got != "" || err == nil || err.Error() != "the trash/ path does not stay in trash/" {
			t.Fatalf("quarantineDest = (%q, %v), want the does-not-stay refusal", got, err)
		}
	})

	t.Run("a target outside the workdir has no relative slot", func(t *testing.T) {
		_, root := agentWorkdir(t)
		got, err := quarantineDest(root, "relative/x.txt")
		if got != "" || err == nil {
			t.Fatalf("quarantineDest = (%q, %v), want an error", got, err)
		}
	})
}

func TestCmdClean(t *testing.T) {
	t.Run("no arguments is a usage refusal", func(t *testing.T) {
		cfg, _ := agentWorkdir(t)
		var out bytes.Buffer
		rc := cmdClean(cfg, nil, &out)
		want := "[ocagent] clean: at least one <path> argument is required\n" +
			"usage: ocagent clean <path>...\n"
		if rc != 2 || out.String() != want {
			t.Fatalf("got (%d, %q), want (2, %q)", rc, out.String(), want)
		}
	})

	t.Run("an identity-less agent refuses before touching anything", func(t *testing.T) {
		var out bytes.Buffer
		rc := cmdClean(Config{Home: "/home"}, []string{"/home/x"}, &out)
		want := "[ocagent] clean: no agent id (OC_ID / OC_TOKEN): cannot tell which workdir is mine\n"
		if rc != 2 || out.String() != want {
			t.Fatalf("got (%d, %q), want (2, %q)", rc, out.String(), want)
		}
	})

	t.Run("a file is moved into quarantine, not deleted", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "x.txt")
		writeFileAt(t, target, "the contents")
		var out bytes.Buffer
		rc := cmdClean(cfg, []string{target}, &out)

		dest := filepath.Join(root, "trash", "tmp", "x.txt")
		want := "[ocagent] clean: " + target + " → " + dest + "\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
		if exists(t, target) {
			t.Fatalf("%s is still there", target)
		}
		if readFileAt(t, dest) != "the contents" {
			t.Fatalf("%s does not hold the original bytes", dest)
		}
	})

	t.Run("a folder is moved whole, contents intact", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		target := filepath.Join(root, "build")
		writeFileAt(t, filepath.Join(target, "deep", "a.txt"), "a")
		writeFileAt(t, filepath.Join(target, "b.txt"), "b")
		var out bytes.Buffer
		rc := cmdClean(cfg, []string{target}, &out)

		dest := filepath.Join(root, "trash", "build")
		if rc != 0 || out.String() != "[ocagent] clean: "+target+" → "+dest+"\n" {
			t.Fatalf("got (%d, %q), want the move line", rc, out.String())
		}
		if exists(t, target) {
			t.Fatalf("%s is still there", target)
		}
		if readFileAt(t, filepath.Join(dest, "deep", "a.txt")) != "a" ||
			readFileAt(t, filepath.Join(dest, "b.txt")) != "b" {
			t.Fatal("the folder's contents did not survive the move")
		}
	})

	t.Run("several paths are all moved and each is reported", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		one := filepath.Join(root, "a.txt")
		two := filepath.Join(root, "tmp", "b.txt")
		writeFileAt(t, one, "1")
		writeFileAt(t, two, "2")
		var out bytes.Buffer
		rc := cmdClean(cfg, []string{one, two}, &out)

		want := "[ocagent] clean: " + one + " → " + filepath.Join(root, "trash", "a.txt") + "\n" +
			"[ocagent] clean: " + two + " → " + filepath.Join(root, "trash", "tmp", "b.txt") + "\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
		if exists(t, one) || exists(t, two) {
			t.Fatal("a source path survived the move")
		}
	})

	t.Run("one bad path among good ones moves NOTHING", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		good := filepath.Join(root, "a.txt")
		writeFileAt(t, good, "1")
		outside := filepath.Join(resolvedTempDir(t), "elsewhere.txt")
		writeFileAt(t, outside, "keep me")

		var out bytes.Buffer
		rc := cmdClean(cfg, []string{good, outside}, &out)
		want := "[ocagent] clean: refused, NOTHING was moved (my workdir is " + root + ")\n" +
			"  " + outside + " — outside my workdir: " + outside + "\n"
		if rc != 2 || out.String() != want {
			t.Fatalf("got (%d, %q), want (2, %q)", rc, out.String(), want)
		}
		if readFileAt(t, good) != "1" || readFileAt(t, outside) != "keep me" {
			t.Fatal("a file moved despite the refusal")
		}
		if exists(t, filepath.Join(root, "trash")) {
			t.Fatal("quarantine was created despite the refusal")
		}
	})

	t.Run("every refusal is listed, not just the first", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		outside := filepath.Join(resolvedTempDir(t), "elsewhere.txt")
		writeFileAt(t, outside, "keep me")
		parked := filepath.Join(root, "trash", "old.txt")
		writeFileAt(t, parked, "parked")

		var out bytes.Buffer
		rc := cmdClean(cfg, []string{outside, root, parked, "  "}, &out)
		want := "[ocagent] clean: refused, NOTHING was moved (my workdir is " + root + ")\n" +
			"  " + outside + " — outside my workdir: " + outside + "\n" +
			"  " + root + " — that is my workdir itself, not something in it: " + root + "\n" +
			"  " + parked + " — that is already in trash/: " + parked + "\n" +
			"     — empty path\n"
		if rc != 2 || out.String() != want {
			t.Fatalf("got (%d, %q), want (2, %q)", rc, out.String(), want)
		}
		if readFileAt(t, parked) != "parked" {
			t.Fatal("a parked file was disturbed")
		}
	})

	t.Run("a path that is already gone is the state the caller asked for", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "gone.txt")
		var out bytes.Buffer
		rc := cmdClean(cfg, []string{target}, &out)
		want := "[ocagent] clean: " + target + " — already gone\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
		if exists(t, filepath.Join(root, "trash")) {
			t.Fatal("quarantine was created for a path that was not there")
		}
	})

	t.Run("cleaning the same relative path twice parks the second copy beside the first", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "x.txt")
		writeFileAt(t, target, "first")
		var out bytes.Buffer
		if rc := cmdClean(cfg, []string{target}, &out); rc != 0 {
			t.Fatalf("first run returned %d", rc)
		}
		writeFileAt(t, target, "second")
		out.Reset()
		rc := cmdClean(cfg, []string{target}, &out)
		dest := filepath.Join(root, "trash", "tmp", "x.txt-2")
		if rc != 0 || out.String() != "[ocagent] clean: "+target+" → "+dest+"\n" {
			t.Fatalf("got (%d, %q), want the -2 slot", rc, out.String())
		}
		if readFileAt(t, filepath.Join(root, "trash", "tmp", "x.txt")) != "first" ||
			readFileAt(t, dest) != "second" {
			t.Fatal("the two parked copies did not both survive")
		}
	})

	t.Run("a symlink is moved as the object it is, leaving its pointee alone", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		pointee := filepath.Join(root, "d.txt")
		writeFileAt(t, pointee, "the real bytes")
		link := filepath.Join(root, "inlink")
		symlinkAt(t, pointee, link)

		var out bytes.Buffer
		dest := filepath.Join(root, "trash", "inlink")
		rc := cmdClean(cfg, []string{link}, &out)
		if rc != 0 || out.String() != "[ocagent] clean: "+link+" → "+dest+"\n" {
			t.Fatalf("got (%d, %q), want the link's own move line", rc, out.String())
		}
		if exists(t, link) {
			t.Fatal("the link is still in place")
		}
		if readFileAt(t, pointee) != "the real bytes" {
			t.Fatal("the pointee was moved instead of the link")
		}
		if target, err := os.Readlink(dest); err != nil || target != pointee {
			t.Fatalf("readlink(%s) = (%q, %v), want the original target", dest, target, err)
		}
	})

	t.Run("a dangling symlink is cleared rather than stranded", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		link := filepath.Join(root, "dangling")
		symlinkAt(t, "/no-such-root-xyz/gone", link)
		var out bytes.Buffer
		dest := filepath.Join(root, "trash", "dangling")
		rc := cmdClean(cfg, []string{link}, &out)
		if rc != 0 || out.String() != "[ocagent] clean: "+link+" → "+dest+"\n" {
			t.Fatalf("got (%d, %q), want the link's own move line", rc, out.String())
		}
		if exists(t, link) || !exists(t, dest) {
			t.Fatal("the dangling link was not parked")
		}
	})

	t.Run("a quarantine slot that cannot be opened is exit 1 and the file stays put", func(t *testing.T) {
		cfg, root := agentWorkdir(t)
		target := filepath.Join(root, "tmp", "x.txt")
		writeFileAt(t, target, "keep me")
		writeFileAt(t, filepath.Join(root, "trash", "tmp"), "i am a file, not a directory")

		var out bytes.Buffer
		rc := cmdClean(cfg, []string{target}, &out)
		if rc != 1 || !strings.HasPrefix(out.String(),
			"[ocagent] clean: "+target+" — cannot inspect the trash/ slot tmp/x.txt: ") {
			t.Fatalf("got (%d, %q), want (1, the cannot-inspect line)", rc, out.String())
		}
		if readFileAt(t, target) != "keep me" {
			t.Fatal("the file did not stay put")
		}
	})
}
