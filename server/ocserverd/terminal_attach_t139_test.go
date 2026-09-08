package main

import "testing"

// The expected strings below are LITERALS on purpose, spelled out character by
// character. Computing them from tmuxSocketForNamespace / agentTmuxSessionName
// would run the seed down the very path under test, and every mutation of that
// path would move the expectation with it — a guard blind to its own mutant.

func TestTerminalAttachCommand(t *testing.T) {
	const agentID = "m-1a2b"

	main := terminalAttachCommand("", agentID)
	if want := "tmux -L officraft attach -t member-m-1a2b"; main != want {
		t.Fatalf("main station:\n got %q\nwant %q", main, want)
	}

	namespaced := terminalAttachCommand("seth", agentID)
	if want := "tmux -L officraft-seth attach -t member-m-1a2b"; namespaced != want {
		t.Fatalf("namespaced station:\n got %q\nwant %q", namespaced, want)
	}

	// The whole reason the field exists: two stations on one host must not hand
	// the owner the same line. An equality here means the namespace stopped
	// reaching the socket, which is the exact defect T-139 retires.
	if main == namespaced {
		t.Fatalf("main and namespaced stations produced ONE command: %q", main)
	}

	// The warden lowercases the session name it spawns under
	// (cli/ocwarden/tmux.go memberSessionName); a mixed-case row must attach to
	// the session that actually exists, not to a name tmux never opened.
	if got, want := terminalAttachCommand("", "M-1A2B"), "tmux -L officraft attach -t member-m-1a2b"; got != want {
		t.Fatalf("mixed-case id:\n got %q\nwant %q", got, want)
	}
}

// Both agent DTOs must serve the command, and must serve the SAME one for the
// same id — an outsource worker has booted under `member-<ow-id>` since P5b, so
// two answers here would mean two rules.
func TestAgentDTOsServeTheTerminalAttachCommand(t *testing.T) {
	for _, tc := range []struct {
		name      string
		namespace string
		wantStaff string
		wantOW    string
	}{
		{
			name:      "main station serves the bare socket",
			namespace: "",
			wantStaff: "tmux -L officraft attach -t member-m-t139",
			wantOW:    "tmux -L officraft attach -t member-ow-t139",
		},
		{
			name:      "namespaced station serves its own socket",
			namespace: "seth",
			wantStaff: "tmux -L officraft-seth attach -t member-m-t139",
			wantOW:    "tmux -L officraft-seth attach -t member-ow-t139",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			s.namespace = tc.namespace

			m := testAgent("m-t139")
			putTestMember(t, s, m)
			if got := s.newMemberDTO(m, "", "", 0).TerminalAttachCommand; got != tc.wantStaff {
				t.Fatalf("MemberDTO:\n got %q\nwant %q", got, tc.wantStaff)
			}
			// The ?fields=light roster goes through the SAME cockpit mapper, so
			// a blank here would tell it "this server is too old" — a claim the
			// full list would contradict on the very next fetch.
			if got := s.newMemberLightDTO(m, "").TerminalAttachCommand; got != tc.wantStaff {
				t.Fatalf("light MemberDTO:\n got %q\nwant %q", got, tc.wantStaff)
			}

			w := OutsourceWorker{ID: "ow-t139", Codename: "O-1", Status: "assigned", Effort: "medium"}
			if err := s.dal.PutOutsourceWorker(w); err != nil {
				t.Fatalf("put worker: %v", err)
			}
			dto := s.projectWorker(w, nil, 0, nowSecs(), nil, nil, nil, func(string) string { return "" }, nil)
			if got := dto.TerminalAttachCommand; got != tc.wantOW {
				t.Fatalf("OutsourceWorkerDTO:\n got %q\nwant %q", got, tc.wantOW)
			}
		})
	}
}
