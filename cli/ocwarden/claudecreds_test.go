package main

import (
	"os"
	"reflect"
	"testing"
)

// credStatRecorder is the stat-only existence seam: it answers from a fixed set and
// records every path it was asked about, so a test can prove the probe never
// looked anywhere else.
type credStatRecorder struct {
	present map[string]bool
	asked   []string
}

func (s *credStatRecorder) exists(path string) bool {
	s.asked = append(s.asked, path)
	return s.present[path]
}

// credArgvRunner is a CmdRunner that records the whole argv it was handed and
// answers with a fixed verdict.
type credArgvRunner struct {
	err   error
	calls [][]string
}

func (r *credArgvRunner) Run(name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return "", r.err
}

func credEnvFunc(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestProbeClaudeCreds(t *testing.T) {
	cases := []struct {
		name        string
		env         map[string]string
		present     map[string]bool
		nilExists   bool
		runner      *credArgvRunner
		goos        string
		want        claudeCredStatus
		wantAsked   []string
		wantRunCall [][]string
	}{
		{
			name: "nothing anywhere on linux",
			env:  map[string]string{"HOME": "/home/seth"},
			goos: "linux",
			want: claudeCredStatus{Present: false, Summary: "cred_file=unset ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name:      "credentials file present",
			env:       map[string]string{"HOME": "/home/seth"},
			present:   map[string]bool{"/home/seth/.claude/.credentials.json": true},
			goos:      "linux",
			want:      claudeCredStatus{Present: true, Summary: "cred_file=SET ANTHROPIC_API_KEY=unset ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name:      "trailing slashes on HOME do not double the separator",
			env:       map[string]string{"HOME": "/home/seth///"},
			present:   map[string]bool{"/home/seth/.claude/.credentials.json": true},
			goos:      "linux",
			want:      claudeCredStatus{Present: true, Summary: "cred_file=SET ANTHROPIC_API_KEY=unset ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name: "no HOME drops the file source entirely",
			env:  map[string]string{},
			goos: "linux",
			want: claudeCredStatus{Present: false, Summary: "ANTHROPIC_API_KEY=unset ANTHROPIC_AUTH_TOKEN=unset " +
				"CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
		},
		{
			name:      "nil exists drops the file source without claiming absence",
			env:       map[string]string{"HOME": "/home/seth"},
			nilExists: true,
			goos:      "linux",
			want: claudeCredStatus{Present: false, Summary: "ANTHROPIC_API_KEY=unset ANTHROPIC_AUTH_TOKEN=unset " +
				"CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
		},
		{
			name:   "darwin keychain item present, queried without -w",
			env:    map[string]string{"HOME": "/Users/seth"},
			runner: &credArgvRunner{},
			goos:   "darwin",
			want: claudeCredStatus{Present: true, Summary: "cred_file=unset keychain=SET ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked:   []string{"/Users/seth/.claude/.credentials.json"},
			wantRunCall: [][]string{{"security", "find-generic-password", "-s", "Claude Code-credentials"}},
		},
		{
			name:   "darwin keychain lookup fails",
			env:    map[string]string{"HOME": "/Users/seth"},
			runner: &credArgvRunner{err: os.ErrNotExist},
			goos:   "darwin",
			want: claudeCredStatus{Present: false, Summary: "cred_file=unset keychain=unset ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked:   []string{"/Users/seth/.claude/.credentials.json"},
			wantRunCall: [][]string{{"security", "find-generic-password", "-s", "Claude Code-credentials"}},
		},
		{
			name: "darwin with no runner drops the keychain source",
			env:  map[string]string{"HOME": "/Users/seth"},
			goos: "darwin",
			want: claudeCredStatus{Present: false, Summary: "cred_file=unset ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/Users/seth/.claude/.credentials.json"},
		},
		{
			name:   "the keychain is never consulted off darwin",
			env:    map[string]string{"HOME": "/home/seth"},
			runner: &credArgvRunner{},
			goos:   "linux",
			want: claudeCredStatus{Present: false, Summary: "cred_file=unset ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name: "api key alone is a credential",
			env:  map[string]string{"HOME": "/home/seth", "ANTHROPIC_API_KEY": "sk-ant-secret"},
			goos: "linux",
			want: claudeCredStatus{Present: true, Summary: "cred_file=unset ANTHROPIC_API_KEY=SET " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name: "a whitespace-only env credential is not a credential",
			env:  map[string]string{"HOME": "/home/seth", "ANTHROPIC_AUTH_TOKEN": "   \t\n"},
			goos: "linux",
			want: claudeCredStatus{Present: false, Summary: "cred_file=unset ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name: "a managed cloud auth flag counts as credentialed",
			env:  map[string]string{"HOME": "/home/seth", "CLAUDE_CODE_USE_BEDROCK": "1"},
			goos: "linux",
			want: claudeCredStatus{Present: true, Summary: "cred_file=unset ANTHROPIC_API_KEY=unset " +
				"ANTHROPIC_AUTH_TOKEN=unset CLAUDE_CODE_USE_BEDROCK=SET CLAUDE_CODE_USE_VERTEX=unset"},
			wantAsked: []string{"/home/seth/.claude/.credentials.json"},
		},
		{
			name: "every source at once",
			env: map[string]string{
				"HOME": "/Users/seth", "ANTHROPIC_API_KEY": "sk-ant-secret",
				"ANTHROPIC_AUTH_TOKEN": "tok", "CLAUDE_CODE_USE_BEDROCK": "1",
				"CLAUDE_CODE_USE_VERTEX": "1",
			},
			present: map[string]bool{"/Users/seth/.claude/.credentials.json": true},
			runner:  &credArgvRunner{},
			goos:    "darwin",
			want: claudeCredStatus{Present: true, Summary: "cred_file=SET keychain=SET ANTHROPIC_API_KEY=SET " +
				"ANTHROPIC_AUTH_TOKEN=SET CLAUDE_CODE_USE_BEDROCK=SET CLAUDE_CODE_USE_VERTEX=SET"},
			wantAsked:   []string{"/Users/seth/.claude/.credentials.json"},
			wantRunCall: [][]string{{"security", "find-generic-password", "-s", "Claude Code-credentials"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stat := &credStatRecorder{present: tc.present}
			exists := stat.exists
			if tc.nilExists {
				exists = nil
			}
			var runner CmdRunner
			if tc.runner != nil {
				runner = tc.runner
			}

			got := probeClaudeCreds(credEnvFunc(tc.env), exists, runner, tc.goos)

			if got != tc.want {
				t.Errorf("probeClaudeCreds = %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(stat.asked, tc.wantAsked) {
				t.Errorf("stat probed %v, want %v", stat.asked, tc.wantAsked)
			}
			if tc.runner != nil && !reflect.DeepEqual(tc.runner.calls, tc.wantRunCall) {
				t.Errorf("runner ran %v, want %v", tc.runner.calls, tc.wantRunCall)
			}
		})
	}
}
