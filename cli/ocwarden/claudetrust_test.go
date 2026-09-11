package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// stageResolvingClaude writes a stand-in for `claude project purge <dir>
// --dry-run` that resolves its config file the way claude 2.1.268 was MEASURED
// to (2026-09-11, both directions): a `.config.json` in the config directory is
// read INSTEAD of `.claude.json`, CLAUDE_CONFIG_DIR moves the directory, and
// otherwise the file is $HOME/.claude.json. It answers from whichever file that
// lands on, so a test can put the warden's write and the child's read on
// different files and watch what the spawn does about it.
//
// The double exists so the shadow-file case is reachable at all in a test; the
// PRODUCTION code deliberately holds no copy of this resolution (claudetrust.go).
func stageResolvingClaude(t *testing.T, path string) string {
	t.Helper()
	return stageBinary(t, path, `#!/bin/sh
# Stand-in for "claude mcp get <name>": resolves its config file the way claude
# 2.1.268 was MEASURED to, then answers with the server names that file holds.
#
# NO EXTERNAL COMMANDS: the test binary runs with a deliberately broken PATH, and
# a stand-in that silently fails to resolve grep answers "not configured" for the
# wrong reason — which is the exact false negative this double exists to detect.
dir=${CLAUDE_CONFIG_DIR:-$HOME/.claude}
if [ -f "$dir/.config.json" ]; then cfg=$dir/.config.json
elif [ -n "$CLAUDE_CONFIG_DIR" ]; then cfg=$CLAUDE_CONFIG_DIR/.claude.json
else cfg=$HOME/.claude.json
fi
names=
while IFS= read -r line; do
  case $line in *oc-pretrust-probe-*)
    n=${line#*\"}
    n=${n%%\"*}
    names="$names $n" ;;
  esac
done < "$cfg"
echo "No MCP server named \"$3\". Configured servers:$names"
`)
}

// configHomeSnapshot lists every file and directory two levels into dir with its
// size, mtime and inode — the before/after evidence that a probe changed nothing.
func configHomeSnapshot(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		st, _ := info.Sys().(*syscall.Stat_t)
		var ino uint64
		if st != nil {
			ino = st.Ino
		}
		out = append(out, fmt.Sprintf("%s size=%d mtime=%d ino=%d", rel, info.Size(), info.ModTime().UnixNano(), ino))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

func TestClaudeTrustProbeArgs(t *testing.T) {
	args := claudeTrustProbeArgs()

	t.Run("the question is a read of the project's MCP config", func(t *testing.T) {
		if joined := strings.Join(args, " "); joined != "mcp get "+claudeTrustProbeMissingName {
			t.Errorf("argv = %q, want a get of a name that is never seeded", joined)
		}
	})

	t.Run("no verb that could change anything", func(t *testing.T) {
		// The predecessor of this probe asked `project purge --dry-run`, and was
		// rejected because a destructive verb on the spawn path bets on --dry-run
		// being honoured — the exact class of assumption this ticket exists
		// because we lost four times. Nothing here may drift back to one.
		for _, a := range args {
			switch a {
			case "purge", "add", "add-json", "add-from-claude-desktop", "remove", "reset", "reset-project-choices", "prune", "clear", "disable", "enable", "install", "uninstall", "update", "--all", "-y", "--yes":
				t.Errorf("argv carries %q, which is not a read: %v", a, args)
			}
		}
		if args[0] != "mcp" || (args[1] != "get" && args[1] != "list") {
			t.Errorf("argv = %v, want an mcp get/list", args)
		}
	})

	t.Run("the name asked about cannot answer for the name seeded", func(t *testing.T) {
		// claude echoes the name it was asked for. A query name carrying the seed
		// prefix would satisfy the witness check out of our own question, and the
		// probe would pass against any file at all.
		if strings.Contains(claudeTrustProbeMissingName, claudeTrustProbeSeedPrefix) {
			t.Errorf("the queried name %q contains the seed prefix %q", claudeTrustProbeMissingName, claudeTrustProbeSeedPrefix)
		}
		echoed := "No MCP server named \"" + claudeTrustProbeMissingName + "\". Configured servers:"
		if strings.Contains(echoed, claudeTrustWitness("/w/m1")) {
			t.Errorf("claude's own echo of the query satisfies the witness: %q", echoed)
		}
	})

	t.Run("the witness is derived from the workdir", func(t *testing.T) {
		if a, b := claudeTrustProbeSeedName("/w/m1"), claudeTrustProbeSeedName("/w/m2"); a == b {
			t.Errorf("two workdirs share the witness %q, so a leftover entry answers for the wrong spawn", a)
		}
	})
}

func TestClaudeTrustProbeScript(t *testing.T) {
	ch := claudeHome{Home: "/Users/owner"}
	script := claudeTrustProbeScript("/bin/claude", "/w/m1", "/w/m1/.oc-env", ch)

	t.Run("the probe runs under the launch line's own environment prologue", func(t *testing.T) {
		// The probe is only evidence about the CHILD if it runs under the child's
		// environment. Sharing the two builders is what makes that true; a copy of
		// them here would pass this test and still drift apart later.
		prologue := claudeChildEnvPrologue("/w/m1", "/w/m1/.oc-env", ch)
		if !strings.HasPrefix(script, prologue) {
			t.Errorf("probe script does not open with the launch prologue:\n%s\nwant prefix\n%s", script, prologue)
		}
	})

	t.Run("the config home is stated before the question is asked", func(t *testing.T) {
		pin := strings.Index(script, "HOME=/Users/owner")
		ask := strings.Index(script, "mcp")
		if pin < 0 || ask < 0 || pin > ask {
			t.Errorf("HOME must be exported before claude is run (HOME=%d, question=%d):\n%s", pin, ask, script)
		}
	})

	t.Run("a redirected config home rides the probe too", func(t *testing.T) {
		redirected := claudeTrustProbeScript("/bin/claude", "/w/m1", "",
			claudeHome{Home: "/Users/owner", ConfigDir: "/tmp/box"})
		if !strings.Contains(redirected, "CLAUDE_CONFIG_DIR=/tmp/box") {
			t.Errorf("the probe must be asked under the same config dir the child gets:\n%s", redirected)
		}
	})
}

func TestVerifyClaudeSeesPretrust(t *testing.T) {
	// The verifier writes a witness into the file pre-trust wrote, so every case
	// needs a real throwaway file — never the suite runner's ~/.claude.json.
	newHome := func(t *testing.T) claudeHome {
		t.Helper()
		home := t.TempDir()
		if err := pretrustWorkdir(filepath.Join(home, ".claude.json"), "/w/m1"); err != nil {
			t.Fatalf("pretrust: %v", err)
		}
		return claudeHome{Home: home}
	}
	witness := claudeTrustProbeSeedName("/w/m1")

	t.Run("a positive answer naming this workdir's witness passes", func(t *testing.T) {
		ch := newHome(t)
		err := verifyClaudeSeesPretrust(func(string) (string, error) {
			return "No MCP server named \"x\". Configured servers: " + witness + "\n", nil
		}, nil, "/bin/claude", "/w/m1", "", ch)
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("the witness is removed again before the child launches", func(t *testing.T) {
		// A probe that leaves its own scaffolding in the owner's config is a probe
		// that changed the thing it measured.
		ch := newHome(t)
		before, err := os.ReadFile(ch.ClaudeJSONPath())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var seen string
		if err := verifyClaudeSeesPretrust(func(string) (string, error) {
			raw, _ := os.ReadFile(ch.ClaudeJSONPath())
			seen = string(raw)
			return "Configured servers: " + witness, nil
		}, nil, "/bin/claude", "/w/m1", "", ch); err != nil {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(seen, witness) {
			t.Errorf("the witness was never in the file while claude was asked:\n%s", seen)
		}
		after, err := os.ReadFile(ch.ClaudeJSONPath())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.Contains(string(after), witness) {
			t.Errorf("the witness survived the probe:\n%s", after)
		}
		if string(after) != string(before) {
			t.Errorf("the config did not come back as it was:\ngot  %s\nwant %s", after, before)
		}
	})

	t.Run("a witness for a different workdir is not an answer about this one", func(t *testing.T) {
		ch := newHome(t)
		err := verifyClaudeSeesPretrust(func(string) (string, error) {
			return "Configured servers: " + claudeTrustProbeSeedName("/w/m2"), nil
		}, nil, "/bin/claude", "/w/m1", "", ch)
		if err == nil {
			t.Fatal("another workdir's witness must not clear this spawn")
		}
	})

	t.Run("a refusal names the file we wrote and the workdir we wrote it for", func(t *testing.T) {
		ch := newHome(t)
		err := verifyClaudeSeesPretrust(func(string) (string, error) {
			return "No MCP server named \"x\". Configured servers:\n", nil
		}, nil, "/bin/claude", "/w/m1", "", ch)
		if err == nil {
			t.Fatal("claude holding none of our witness must refuse the spawn")
		}
		for _, want := range []string{ch.ClaudeJSONPath(), "/w/m1", claudeShadowConfigName} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q does not name %q", err, want)
			}
		}
	})

	t.Run("a question that could not be asked refuses too", func(t *testing.T) {
		// Every UNKNOWN lands on the refusing side: a claude that crashed, timed
		// out, changed its output, or dropped the subcommand all arrive here, and
		// a best-effort pass on any of them is the silent failure this ticket
		// exists to kill.
		cases := map[string]func(claudeHome) error{
			"exec failed": func(ch claudeHome) error {
				return verifyClaudeSeesPretrust(func(string) (string, error) {
					return "", errors.New("signal: killed")
				}, nil, "/bin/claude", "/w/m1", "", ch)
			},
			"output format changed": func(ch claudeHome) error {
				return verifyClaudeSeesPretrust(func(string) (string, error) {
					return "error: unknown command 'get'\n", nil
				}, nil, "/bin/claude", "/w/m1", "", ch)
			},
			"no claude resolved": func(ch claudeHome) error {
				return verifyClaudeSeesPretrust(func(string) (string, error) {
					return "Configured servers: " + witness, nil
				}, nil, "", "/w/m1", "", ch)
			},
			"no way to run it": func(ch claudeHome) error {
				return verifyClaudeSeesPretrust(nil, nil, "/bin/claude", "/w/m1", "", ch)
			},
			"the witness cannot be written": func(claudeHome) error {
				return verifyClaudeSeesPretrust(func(string) (string, error) {
					return "Configured servers: " + witness, nil
				}, nil, "/bin/claude", "/w/m1", "", claudeHome{Home: filepath.Join(t.TempDir(), "no", "such", "dir")})
			},
		}
		for name, run := range cases {
			if err := run(newHome(t)); err == nil {
				t.Errorf("%s: err = nil, want a refusal", name)
			}
		}
	})
}

// TestPretrustIsVerifiedAgainstTheFileClaudeReallyReads is THE falsifiable
// condition of the fifth shape. It runs the real probe, in a real shell, against
// a claude stand-in that resolves its config file the measured way — so the case
// the fourth review broke (a `.config.json` beside the file we write) is
// reachable, and the spawn's answer to it is observable.
func TestPretrustIsVerifiedAgainstTheFileClaudeReallyReads(t *testing.T) {
	box := t.TempDir()
	home := filepath.Join(box, "home")
	configDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	claudeBin := stageResolvingClaude(t, filepath.Join(box, "bin", "claude"))
	workdir := filepath.Join(box, "agents", "m1")
	ch := claudeHome{Home: home}
	runner := &wardenRunner{shellPassthrough: true}
	verify := newClaudePretrustVerifier(runner, "officraft", claudeBin, ch, nil)

	if err := pretrustWorkdir(ch.ClaudeJSONPath(), workdir); err != nil {
		t.Fatalf("pretrust: %v", err)
	}
	if err := verify(workdir, ""); err != nil {
		t.Fatalf("with nothing shadowing it, the written file IS the one claude reads: %v", err)
	}

	// The fourth review's finding: this file's mere EXISTENCE moves the read.
	// Nothing about the environment changed, so every environment-side check the
	// first four shapes had still reports success here.
	shadow := filepath.Join(configDir, claudeShadowConfigName)
	if err := os.WriteFile(shadow, []byte(`{"projects":{}}`), 0o600); err != nil {
		t.Fatalf("seed shadow: %v", err)
	}
	err := verify(workdir, "")
	if err == nil {
		t.Fatal("a .config.json beside the trust file moves the read; the spawn must refuse instead of launching a member onto the trust dialog")
	}
	if !strings.Contains(err.Error(), ch.ClaudeJSONPath()) || !strings.Contains(err.Error(), workdir) {
		t.Errorf("refusal %q must name both the file written and the workdir", err)
	}

	// Put the flag AND the witness into the shadow — the file claude really reads
	// — and the same question answers yes. So the refusal above is about WHICH
	// FILE, not about the probe being unable to answer at all.
	//
	// Note what this also shows: claudeHome always names `.claude.json`, so the
	// warden cannot be pointed AT a `.config.json`. When one exists there is no
	// correct file for the warden to write, and refusing is the only honest move.
	if err := pretrustWorkdir(shadow, workdir); err != nil {
		t.Fatalf("pretrust shadow: %v", err)
	}
	if err := seedTrustProbeWitness(shadow, workdir, claudeTrustProbeSeedName(workdir)); err != nil {
		t.Fatalf("seed witness into the shadow: %v", err)
	}
	out, _ := runner.Run("/bin/sh", "-c", claudeTrustProbeScript(claudeBin, workdir, "", ch))
	if !strings.Contains(out, claudeTrustWitness(workdir)) {
		t.Errorf("with the witness in the file claude reads, the answer must be yes; got: %s", out)
	}

	// Remove the shadow and the whole verifier passes again — so the refusal was
	// caused by that file, not by residue from the attempt.
	if err := os.Remove(shadow); err != nil {
		t.Fatalf("remove shadow: %v", err)
	}
	if err := verify(workdir, ""); err != nil {
		t.Errorf("with the shadow gone the written file is the read file again: %v", err)
	}
}

func TestTmuxLaunchShell(t *testing.T) {
	t.Run("the shell tmux states is the shell the probe uses", func(t *testing.T) {
		r := &wardenRunner{script: map[string]wardenRun{
			"tmux -L officraft show-options -gv default-shell": {out: "/bin/zsh\n"},
		}}
		if got := tmuxLaunchShell(r, "officraft", nil); got != "/bin/zsh" {
			t.Errorf("shell = %q, want /bin/zsh", got)
		}
	})

	t.Run("a tmux that cannot answer falls back and says so", func(t *testing.T) {
		var logs []string
		r := &wardenRunner{fallback: wardenRun{err: errors.New("no server running")}}
		got := tmuxLaunchShell(r, "officraft", func(f string, a ...any) { logs = append(logs, f) })
		if got != claudeTrustProbeFallbackShell {
			t.Errorf("shell = %q, want the POSIX fallback", got)
		}
		if len(logs) != 1 {
			t.Errorf("falling back to a different shell than the child gets must be logged, got %v", logs)
		}
	})

	t.Run("a relative answer is not a shell", func(t *testing.T) {
		r := &wardenRunner{script: map[string]wardenRun{
			"tmux -L officraft show-options -gv default-shell": {out: "zsh\n"},
		}}
		if got := tmuxLaunchShell(r, "officraft", nil); got != claudeTrustProbeFallbackShell {
			t.Errorf("shell = %q, want the POSIX fallback", got)
		}
	})
}

// TestRealClaudeAnswersTheProbe is the same falsifiable condition run against
// the claude binary THIS HOST would actually spawn, not a double. It is the only
// thing in the suite that can notice claude changing the answer shape the probe
// reads — the drift this design deliberately turns into a loud refusal — so it
// is worth the ~0.3s and the one subprocess.
//
// It also carries the NON-DESTRUCTIVENESS evidence: a full snapshot of the config
// home (every path, size, mtime, inode) taken either side of a probe, compared
// exactly. "I ran it twice and nothing looked broken" is not that.
//
// It skips where no claude resolves (CI) and touches only a throwaway HOME.
func TestRealClaudeAnswersTheProbe(t *testing.T) {
	bin := resolveClaudeBin(os.Getenv)
	if bin == "" {
		t.Skip("no claude on this host")
	}
	// REAL PATH, not t.TempDir()'s /var/folders symlink. claude keys a project by
	// the cwd it resolves, so a workdir reached through a symlink is looked up
	// under a different key than pre-trust wrote — the pre-existing symlink hole
	// (out of scope here, C2). Production agent workdirs are under the real $HOME;
	// this test must not fail for a reason the ticket is not about.
	box, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve tempdir: %v", err)
	}
	home := filepath.Join(box, "home")
	configDir := filepath.Join(home, ".claude")
	workdir := filepath.Join(box, "agents", "m1")
	for _, dir := range []string{configDir, workdir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	ch := claudeHome{Home: home}
	// The test binary runs with a deliberately broken PATH; claude needs a real
	// one. HOME is stated here too so a failure to export it inside the script
	// cannot be covered by the ambient one.
	ask := func(script string) (string, error) {
		cmd := exec.Command("/bin/zsh", "-c", script)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// STATE 1 — only .claude.json: the file we write is the file claude reads.
	if err := pretrustWorkdir(ch.ClaudeJSONPath(), workdir); err != nil {
		t.Fatalf("pretrust: %v", err)
	}
	if err := verifyClaudeSeesPretrust(ask, nil, bin, workdir, "", ch); err != nil {
		t.Fatalf("the real claude cannot see the witness we wrote for it: %v", err)
	}

	// NON-DESTRUCTIVENESS, in two halves.
	//
	// (a) THE CLAUDE INVOCATION changes nothing at all. Snapshot every path, size,
	// mtime and inode two levels into the config home, run the question claude is
	// actually asked, snapshot again, compare exactly — no new file, no removed
	// file, no rewrite, not even a backups/ rotation.
	seed := claudeTrustProbeSeedName(workdir)
	if err := seedTrustProbeWitness(ch.ClaudeJSONPath(), workdir, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := configHomeSnapshot(t, home)
	out, _ := ask(claudeTrustProbeScript(bin, workdir, "", ch))
	after := configHomeSnapshot(t, home)
	if !strings.Contains(out, seed) {
		t.Fatalf("the probe did not answer, so this proves nothing: %s", out)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("asking claude changed the config home.\nbefore:\n%s\nafter:\n%s",
			strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
	if err := clearTrustProbeWitness(ch.ClaudeJSONPath(), workdir, seed); err != nil {
		t.Fatalf("clear: %v", err)
	}

	// (b) THE WARDEN'S OWN seed/clear leaves the file byte-identical. Those two
	// writes are the only reason a full verify touches the config at all, and an
	// atomic rewrite changes mtime and inode, so the honest comparison for this
	// half is the bytes.
	wrote, err := os.ReadFile(ch.ClaudeJSONPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := verifyClaudeSeesPretrust(ask, nil, bin, workdir, "", ch); err != nil {
		t.Fatalf("second probe: %v", err)
	}
	back, err := os.ReadFile(ch.ClaudeJSONPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(back) != string(wrote) {
		t.Errorf("a full probe did not leave the config as it found it:\ngot  %s\nwant %s", back, wrote)
	}

	// STATE 2 — a .config.json appears: the read moves, nothing in the
	// environment changed, and the spawn must refuse by name.
	shadow := filepath.Join(configDir, claudeShadowConfigName)
	if err := os.WriteFile(shadow, []byte(`{"projects":{}}`), 0o600); err != nil {
		t.Fatalf("seed shadow: %v", err)
	}
	if err := verifyClaudeSeesPretrust(ask, nil, bin, workdir, "", ch); err == nil {
		t.Fatal("the real claude reads the shadow file, and the spawn went ahead anyway")
	}

	// STATE 3 — the flag and the witness are put into the shadow, the file the
	// real claude reads, and the same question answers yes. The refusal above was
	// about WHICH FILE, not about the probe having stopped working.
	if err := pretrustWorkdir(shadow, workdir); err != nil {
		t.Fatalf("pretrust shadow: %v", err)
	}
	if err := seedTrustProbeWitness(shadow, workdir, claudeTrustProbeSeedName(workdir)); err != nil {
		t.Fatalf("seed witness into the shadow: %v", err)
	}
	moved, _ := ask(claudeTrustProbeScript(bin, workdir, "", ch))
	if !strings.Contains(moved, claudeTrustWitness(workdir)) {
		t.Errorf("with the witness in the file the real claude reads, the answer must be yes; got: %s", moved)
	}
}
