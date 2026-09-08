// Skeleton generated from server/ocserverd/browser.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		wantName string
		wantArg  string
		wantErr  string
	}{
		{name: "macOS uses open", goos: "darwin", wantName: "open", wantArg: "http://127.0.0.1:7757/?code=claim"},
		{name: "Linux uses xdg-open", goos: "linux", wantName: "xdg-open", wantArg: "http://127.0.0.1:7757/?code=claim"},
		{name: "an unsupported platform returns an explicit error", goos: "plan9", wantErr: `no browser opener for GOOS "plan9"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			gotName := ""
			gotArg := ""
			b := browserOpener{
				goos: tc.goos,
				run: func(name string, arg ...string) error {
					called = true
					gotName = name
					if len(arg) == 1 {
						gotArg = arg[0]
					}
					return nil
				},
			}
			err := b.open(tc.wantArg)
			if tc.wantErr != "" {
				if called {
					t.Fatal("unsupported GOOS must not invoke the command seam")
				}
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("open error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !called || gotName != tc.wantName || gotArg != tc.wantArg {
				t.Fatalf("command = called:%v name:%q arg:%q, want %q %q", called, gotName, gotArg, tc.wantName, tc.wantArg)
			}
		})
	}

	t.Run("a command failure is returned to the caller", func(t *testing.T) {
		want := errors.New("opener failed")
		b := browserOpener{goos: "linux", run: func(string, ...string) error { return want }}
		if got := b.open("http://127.0.0.1:7757/?code=claim"); !errors.Is(got, want) {
			t.Fatalf("open error = %v, want %v", got, want)
		}
	})
}

func TestStdoutIsTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = saved
		_ = w.Close()
		_ = r.Close()
	})

	if got := stdoutIsTerminal(); got {
		t.Fatal("stdoutIsTerminal() = true for a pipe, want false")
	}
}

func TestPopFirstRunBrowser(t *testing.T) {
	const setupURL = "http://127.0.0.1:7757/?code=claim"

	t.Run("a successful open prints only the confirmation", func(t *testing.T) {
		called := false
		b := browserOpener{
			goos: "linux",
			run: func(name string, arg ...string) error {
				called = true
				if name != "xdg-open" || len(arg) != 1 || arg[0] != setupURL {
					t.Fatalf("browser command = %q %q, want xdg-open %q", name, arg, setupURL)
				}
				return nil
			},
		}
		var out strings.Builder
		popFirstRunBrowser(b, setupURL, &out)
		if !called {
			t.Fatal("successful browser open did not invoke the command seam")
		}
		if got := out.String(); got != "[ocserverd] opened the setup page in your browser — choose a password there to claim the server\n" {
			t.Fatalf("output = %q, want the no-token confirmation", got)
		}
	})

	t.Run("a failed open prints the full clickable URL", func(t *testing.T) {
		var out strings.Builder
		b := browserOpener{
			goos: "linux",
			run:  func(string, ...string) error { return errors.New("no display") },
		}
		popFirstRunBrowser(b, setupURL, &out)
		if got := out.String(); got != "[ocserverd]   "+setupURL+"\n" {
			t.Fatalf("output = %q, want the full clickable URL", got)
		}
	})
}
