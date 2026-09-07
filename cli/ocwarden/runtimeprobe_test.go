package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectRuntimeCapabilities(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "nothing-here"))
	claudeBin := stageBinary(t, filepath.Join(root, "bin", "claude"), "#!/bin/sh\n")
	codexBin := stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n")

	versionArgv := codexBin + " --version"
	statusArgv := codexBin + " login status"
	loginRefused := errors.New("exit status 1: not logged in")

	cases := []struct {
		name     string
		env      map[string]string
		script   map[string]wardenRun
		claude   map[string]any
		want     map[string]any
		wantRuns []string
		wantLog  []string
	}{
		{
			name:     "neither CLI is on the host",
			env:      map[string]string{"HOME": root},
			claude:   map[string]any{"cred_file": true, "keychain": true},
			want:     map[string]any{"claude": map[string]any{"installed": false}, "codex": map[string]any{"installed": false}},
			wantRuns: nil,
		},
		{
			name:   "an absent claude still reports the probed version, never a login verdict",
			env:    map[string]string{"HOME": root},
			claude: map[string]any{"version": "2.1.211", "cred_file": true},
			want: map[string]any{
				"claude": map[string]any{"installed": false, "version": "2.1.211"},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "a credential file is evidence of a claude login",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"version": "2.1.211", "cred_file": true, "keychain": false},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "version": "2.1.211", "logged_in": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "a keychain item alone is evidence of a claude login",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"cred_file": false, "keychain": true},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "logged_in": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "finding no claude credential reports unknown, never logged_in false",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"version": "", "cred_file": false, "keychain": false},
			want: map[string]any{
				"claude": map[string]any{"installed": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name: "codex answers its version and its login status",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {out: "codex-cli 0.52.0\n"},
				statusArgv:  {out: "Logged in as eva\n"},
			},
			claude: map[string]any{},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "version": "0.52.0", "logged_in": true},
			},
			wantRuns: []string{versionArgv, statusArgv},
		},
		{
			name: "a refused codex login status is a measured false, and the error reaches the log",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {out: "codex-cli 0.52.0\n"},
				statusArgv:  {err: loginRefused},
			},
			claude: map[string]any{},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "version": "0.52.0", "logged_in": false},
			},
			wantRuns: []string{versionArgv, statusArgv},
			wantLog: []string{fmt.Sprintf(
				"[ocwarden runtimeprobe] codex login status failed (bin=%s): exit status 1: not logged in", codexBin)},
		},
		{
			name: "an unanswerable codex version drops the key and still measures the login",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {err: errors.New("signal: killed")},
				statusArgv:  {out: "Logged in as eva\n"},
			},
			claude: map[string]any{},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "logged_in": true},
			},
			wantRuns: []string{versionArgv, statusArgv},
		},
		{
			name: "a blank codex version line yields no version",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {out: "   \n"},
				statusArgv:  {out: "ok"},
			},
			claude: map[string]any{},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "logged_in": true},
			},
			wantRuns: []string{versionArgv, statusArgv},
		},
	}

	for _, c := range cases {
		runner := &wardenRunner{script: c.script, fallback: wardenRun{err: errors.New("unscripted argv")}}
		var log []string
		logf := func(format string, args ...any) { log = append(log, fmt.Sprintf(format, args...)) }

		got := collectRuntimeCapabilities(envMap(c.env), runner, c.claude, logf)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: capabilities =\n  %#v\nwant\n  %#v", c.name, got, c.want)
		}
		if !reflect.DeepEqual(runner.calls, c.wantRuns) {
			t.Errorf("%s: ran %v, want %v", c.name, runner.calls, c.wantRuns)
		}
		if !reflect.DeepEqual(log, c.wantLog) {
			t.Errorf("%s: log =\n  %#v\nwant\n  %#v", c.name, log, c.wantLog)
		}
	}

	silent := &wardenRunner{fallback: wardenRun{err: errors.New("exit status 1")}}
	got := collectRuntimeCapabilities(
		envMap(map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin}), silent, nil, nil)
	want := map[string]any{
		"claude": map[string]any{"installed": false},
		"codex":  map[string]any{"installed": true, "logged_in": false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("an unwired log sink must not change the report: capabilities = %#v, want %#v", got, want)
	}
}
