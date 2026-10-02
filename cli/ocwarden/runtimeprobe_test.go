package main

import (
	"errors"
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
	yes, no := true, false

	cases := []struct {
		name     string
		env      map[string]string
		script   map[string]wardenRun
		claude   map[string]any
		login    loginState
		want     map[string]any
		wantRuns []string
	}{
		{
			name:     "under neither CLI on the host, both report not installed and no login verdict",
			env:      map[string]string{"HOME": root},
			claude:   map[string]any{"cred_file": true, "keychain": true},
			login:    loginState{Claude: &yes, Codex: &yes},
			want:     map[string]any{"claude": map[string]any{"installed": false}, "codex": map[string]any{"installed": false}},
			wantRuns: nil,
		},
		{
			name:   "under an absent claude, the probed version is still reported and no login verdict",
			env:    map[string]string{"HOME": root},
			claude: map[string]any{"version": "2.1.211", "cred_file": true},
			login:  loginState{Claude: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": false, "version": "2.1.211", "below_notify_minimum": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "under a logged-in auth status, claude reports logged_in true",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"version": "2.1.211", "cred_file": false, "keychain": false},
			login:  loginState{Claude: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "version": "2.1.211", "logged_in": true, "below_notify_minimum": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "under a claude exactly at the notification mod's minimum, below_notify_minimum is false",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"version": "2.1.287"},
			login:  loginState{Claude: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "version": "2.1.287", "logged_in": true, "below_notify_minimum": false},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "under a claude newer than the minimum in an earlier component, below_notify_minimum is false",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"version": "2.2.0"},
			login:  loginState{Claude: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "version": "2.2.0", "logged_in": true, "below_notify_minimum": false},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "under a claude version that is not dotted numbers, below_notify_minimum is absent",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"version": "2.1.290-beta"},
			login:  loginState{Claude: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "version": "2.1.290-beta", "logged_in": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "under a logged-out auth status, claude reports logged_in false even with credentials present",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"cred_file": true, "keychain": true},
			login:  loginState{Claude: &no},
			want: map[string]any{
				"claude": map[string]any{"installed": true, "logged_in": false},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name:   "under an unknown auth status, claude omits logged_in even with credentials present",
			env:    map[string]string{"HOME": root, "OC_CLAUDE_BIN": claudeBin},
			claude: map[string]any{"cred_file": true, "keychain": true},
			login:  loginState{},
			want: map[string]any{
				"claude": map[string]any{"installed": true},
				"codex":  map[string]any{"installed": false},
			},
		},
		{
			name: "under a logged-in codex, its version, login and model-family support are reported without a login exec",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {out: "codex-cli 0.52.0\n"},
			},
			claude: map[string]any{},
			login:  loginState{Codex: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "version": "0.52.0", "logged_in": true, "model_families": true},
			},
			wantRuns: []string{versionArgv},
		},
		{
			name: "under a refused codex login check, codex reports logged_in false",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {out: "codex-cli 0.52.0\n"},
			},
			claude: map[string]any{},
			login:  loginState{Codex: &no},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "version": "0.52.0", "logged_in": false, "model_families": true},
			},
			wantRuns: []string{versionArgv},
		},
		{
			name: "under an unanswerable codex version, the version key is dropped and the login still reported",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {err: errors.New("signal: killed")},
			},
			claude: map[string]any{},
			login:  loginState{Codex: &yes},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "logged_in": true, "model_families": true},
			},
			wantRuns: []string{versionArgv},
		},
		{
			name: "under a blank codex version line and no login verdict, codex reports neither",
			env:  map[string]string{"HOME": root, "OC_CODEX_BIN": codexBin},
			script: map[string]wardenRun{
				versionArgv: {out: "   \n"},
			},
			claude: map[string]any{},
			login:  loginState{},
			want: map[string]any{
				"claude": map[string]any{"installed": false},
				"codex":  map[string]any{"installed": true, "model_families": true},
			},
			wantRuns: []string{versionArgv},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &wardenRunner{script: c.script, fallback: wardenRun{err: errors.New("unscripted argv")}}
			got := collectRuntimeCapabilities(envMap(c.env), runner, c.claude, c.login)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("capabilities =\n  %#v\nwant\n  %#v", got, c.want)
			}
			if !reflect.DeepEqual(runner.calls, c.wantRuns) {
				t.Errorf("ran %v, want %v", runner.calls, c.wantRuns)
			}
		})
	}
}
