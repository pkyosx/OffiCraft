package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	keychainToken = "sk-ant-oat01-keychain-secret"
	fileToken     = "sk-ant-oat01-file-secret"
)

func storedCredential(token, subscription string, expiresAtMs int64) string {
	raw, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": token, "refreshToken": "sk-ant-ort01-refresh-secret",
		"expiresAt": expiresAtMs, "scopes": []string{"user:inference"}, "subscriptionType": subscription,
	}})
	return string(raw)
}

func writeCredentialFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCredentialSourceRead(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	keychainItem := "security find-generic-password -a seth -s Claude Code-credentials -w"

	t.Run("on macOS with both stores holding a login, the keychain login is read", func(t *testing.T) {
		home := t.TempDir()
		writeCredentialFile(t, filepath.Join(home, ".claude"), storedCredential(fileToken, "team", future))
		runner := &wardenRunner{script: map[string]wardenRun{keychainItem: {out: storedCredential(keychainToken, "max", future) + "\n"}}}
		source := claudeCredentialSource{goos: "darwin", home: claudeHome{Home: home}, user: "seth", readFile: os.ReadFile, runner: runner}

		cred, ok := source.read()

		if !ok || cred.store != "keychain" || cred.accessToken != keychainToken || cred.subscriptionType != "max" {
			t.Errorf("read = (store %q, subscription %q, keychain token %v, ok %v), want the keychain login",
				cred.store, cred.subscriptionType, cred.accessToken == keychainToken, ok)
		}
	})

	t.Run("on macOS with no keychain item or an unreadable one, the plaintext file is read", func(t *testing.T) {
		for name, keychain := range map[string]wardenRun{
			"no item":          {err: errors.New("exit status 44: security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.")},
			"not a credential": {out: "c0ffee\n"},
			"no access token":  {out: `{"claudeAiOauth":{"subscriptionType":"max"}}`},
		} {
			t.Run(name, func(t *testing.T) {
				home := t.TempDir()
				writeCredentialFile(t, filepath.Join(home, ".claude"), storedCredential(fileToken, "team", future))
				runner := &wardenRunner{script: map[string]wardenRun{keychainItem: keychain}}
				source := claudeCredentialSource{goos: "darwin", home: claudeHome{Home: home}, user: "seth", readFile: os.ReadFile, runner: runner}

				cred, ok := source.read()

				if !ok || cred.store != "file" || cred.accessToken != fileToken {
					t.Errorf("read = (store %q, ok %v), want the file login", cred.store, ok)
				}
			})
		}
	})

	t.Run("on Linux only the plaintext file is read and no keychain is asked", func(t *testing.T) {
		home := t.TempDir()
		writeCredentialFile(t, filepath.Join(home, ".claude"), storedCredential(fileToken, "pro", future))
		runner := &wardenRunner{}
		source := claudeCredentialSource{goos: "linux", home: claudeHome{Home: home}, user: "seth", readFile: os.ReadFile, runner: runner}

		cred, ok := source.read()

		if !ok || cred.store != "file" || cred.accessToken != fileToken {
			t.Errorf("read = (store %q, ok %v), want the file login", cred.store, ok)
		}
		if len(runner.calls) != 0 {
			t.Errorf("ran %q on Linux, want nothing", runner.calls)
		}
	})

	t.Run("under a redirected config dir, the item suffixed with that dir's hash and the file inside it are read", func(t *testing.T) {
		home, box := t.TempDir(), t.TempDir()
		writeCredentialFile(t, filepath.Join(home, ".claude"), storedCredential("sk-ant-oat01-home-login", "max", future))
		writeCredentialFile(t, box, storedCredential(fileToken, "team", future))
		source := claudeCredentialSource{goos: "darwin", home: claudeHome{Home: home, ConfigDir: box}, user: "seth",
			readFile: os.ReadFile, runner: &wardenRunner{fallback: wardenRun{err: errors.New("exit status 44")}}}

		if got := source.keychainService(); !strings.HasPrefix(got, "Claude Code-credentials-") || len(got) != len("Claude Code-credentials-")+8 {
			t.Errorf("keychainService = %q, want the item suffixed with 8 hex digits", got)
		}
		cred, ok := source.read()
		if !ok || cred.store != "file" || cred.accessToken != fileToken {
			t.Errorf("read = (store %q, ok %v), want the file in the redirected dir", cred.store, ok)
		}
	})

	t.Run("with neither store holding a login, nothing is read", func(t *testing.T) {
		source := claudeCredentialSource{goos: "darwin", home: claudeHome{Home: t.TempDir()}, user: "seth",
			readFile: os.ReadFile, runner: &wardenRunner{fallback: wardenRun{err: errors.New("exit status 44")}}}
		if _, ok := source.read(); ok {
			t.Error("read answered a login, want none")
		}
	})
}

type anthropicStub struct {
	mu       sync.Mutex
	profile  func(w http.ResponseWriter)
	usage    func(w http.ResponseWriter)
	requests []string
}

func (s *anthropicStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization")+" beta="+r.Header.Get("anthropic-beta"))
	s.mu.Unlock()
	switch r.URL.Path {
	case "/api/oauth/profile":
		s.profile(w)
	case "/api/oauth/usage":
		s.usage(w)
	default:
		http.NotFound(w, r)
	}
}

func answer(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = w.Write([]byte(body)) }
}

func refuse(status int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(status) }
}

const (
	personalProfile = `{"account":{"uuid":"893ef8f2","email":"seth@x.io","display_name":"Seth"},` +
		`"organization":{"uuid":"0cea9af2","name":"Personal","organization_type":"claude_max"}}`
	personalUsage = `{"five_hour":{"utilization":7.0,"resets_at":"2026-11-27T13:59:59.943648+00:00"},` +
		`"seven_day":{"utilization":79.0,"resets_at":"2026-11-29T10:00:00+00:00"},"seven_day_opus":null}`
)

func TestClaudeAccountMeterMeasure(t *testing.T) {
	now := time.Date(2026, 11, 27, 11, 0, 0, 0, time.UTC)
	meterFor := func(t *testing.T, stub *anthropicStub, credential string) claudeAccountMeter {
		t.Helper()
		srv := httptest.NewServer(stub)
		t.Cleanup(srv.Close)
		home := t.TempDir()
		if credential != "" {
			writeCredentialFile(t, filepath.Join(home, ".claude"), credential)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude.json"),
			[]byte(`{"oauthAccount":{"accountUuid":"893ef8f2","organizationUuid":"89effd96","emailAddress":"seth@x.io","organizationName":"GoFreight"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return claudeAccountMeter{
			source:    claudeCredentialSource{goos: "linux", home: claudeHome{Home: home}, user: "seth", readFile: os.ReadFile},
			installed: func() bool { return true },
			client:    srv.Client(),
			base:      srv.URL,
			now:       func() time.Time { return now },
		}
	}
	valid := storedCredential(fileToken, "max", now.Add(time.Hour).UnixMilli())

	t.Run("a valid login is measured as the profile's account with the usage endpoint's windows", func(t *testing.T) {
		stub := &anthropicStub{profile: answer(personalProfile), usage: answer(personalUsage)}

		got, ok := meterFor(t, stub, valid).measure()

		want := claudeAccountMeasurement{
			Account: "893ef8f2/0cea9af2", Label: "seth@x.io(Personal)", SubscriptionType: "max", Store: "file",
			MeasuredAt: now,
			FiveHour:   map[string]any{"used_percentage": 7.0, "resets_at": float64(time.Date(2026, 11, 27, 13, 59, 59, 0, time.UTC).Unix())},
			SevenDay:   map[string]any{"used_percentage": 79.0, "resets_at": float64(time.Date(2026, 11, 29, 10, 0, 0, 0, time.UTC).Unix())},
		}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("measure = %+v (ok %v)\nwant %+v", got, ok, want)
		}
		wantRequests := []string{
			"GET /api/oauth/profile auth=Bearer " + fileToken + " beta=oauth-2025-04-20",
			"GET /api/oauth/usage auth=Bearer " + fileToken + " beta=oauth-2025-04-20",
		}
		if !reflect.DeepEqual(stub.requests, wantRequests) {
			t.Errorf("requests = %q, want %q", stub.requests, wantRequests)
		}
	})

	t.Run("a window the usage endpoint gives no number for is left out", func(t *testing.T) {
		stub := &anthropicStub{profile: answer(personalProfile),
			usage: answer(`{"five_hour":{"utilization":null,"resets_at":null},"seven_day":{"utilization":3,"resets_at":"2026-11-29T10:00:00Z"}}`)}

		got, _ := meterFor(t, stub, valid).measure()

		if got.FiveHour != nil || got.SevenDay["used_percentage"] != 3.0 {
			t.Errorf("windows = five_hour %v, seven_day %v; want no five_hour and the seven_day number", got.FiveHour, got.SevenDay)
		}
	})

	t.Run("an expired access token is reported unmeasured without calling Anthropic or refreshing it", func(t *testing.T) {
		stub := &anthropicStub{profile: answer(personalProfile), usage: answer(personalUsage)}
		expired := storedCredential(fileToken, "max", now.Add(-time.Minute).UnixMilli())
		meter := meterFor(t, stub, expired)

		got, ok := meter.measure()

		want := claudeAccountMeasurement{Store: "file", SubscriptionType: "max", MeasuredAt: now, UnmeasuredReason: "token_expired"}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("measure = %+v, want %+v", got, want)
		}
		if len(stub.requests) != 0 {
			t.Errorf("requests = %q, want none", stub.requests)
		}
		raw, _ := os.ReadFile(filepath.Join(meter.source.home.Home, ".claude", ".credentials.json"))
		if string(raw) != expired {
			t.Error("the credential file was rewritten")
		}
	})

	t.Run("no stored login is reported unmeasured", func(t *testing.T) {
		stub := &anthropicStub{profile: answer(personalProfile), usage: answer(personalUsage)}

		got, ok := meterFor(t, stub, "").measure()

		if want := (claudeAccountMeasurement{MeasuredAt: now, UnmeasuredReason: "no_credential"}); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("measure = %+v, want %+v", got, want)
		}
	})

	t.Run("a failed profile call leaves the account unknown, never the one ~/.claude.json names, and the usage is not asked", func(t *testing.T) {
		for name, profile := range map[string]func(http.ResponseWriter){
			"refused":         refuse(http.StatusUnauthorized),
			"unreadable":      answer(`<html>`),
			"no account uuid": answer(`{"account":{"email":"seth@x.io"},"organization":{"uuid":"0cea9af2"}}`),
		} {
			t.Run(name, func(t *testing.T) {
				stub := &anthropicStub{profile: profile, usage: answer(personalUsage)}

				got, ok := meterFor(t, stub, valid).measure()

				if !ok || got.Account != "" || got.Label != "" || got.UnmeasuredReason != "profile_failed" || got.FiveHour != nil || got.SevenDay != nil {
					t.Errorf("measure = %+v, want profile_failed with no account and no numbers", got)
				}
				if len(stub.requests) != 1 {
					t.Errorf("requests = %q, want the profile call only", stub.requests)
				}
			})
		}
	})

	t.Run("a failed usage call keeps the identified account and reports no numbers", func(t *testing.T) {
		stub := &anthropicStub{profile: answer(personalProfile), usage: refuse(http.StatusTooManyRequests)}

		got, _ := meterFor(t, stub, valid).measure()

		want := claudeAccountMeasurement{
			Account: "893ef8f2/0cea9af2", Label: "seth@x.io(Personal)", SubscriptionType: "max", Store: "file",
			MeasuredAt: now, UnmeasuredReason: "usage_failed", Detail: "/api/oauth/usage: HTTP 429",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure = %+v\nwant %+v", got, want)
		}
	})

	t.Run("a host without claude has nothing to measure", func(t *testing.T) {
		stub := &anthropicStub{profile: answer(personalProfile), usage: answer(personalUsage)}
		meter := meterFor(t, stub, valid)
		meter.installed = func() bool { return false }

		if _, ok := meter.measure(); ok || len(stub.requests) != 0 {
			t.Errorf("measure ok=%v with requests %q, want nothing measured", ok, stub.requests)
		}
	})
}

func TestClaudeAccountReporterMeasureNow(t *testing.T) {
	measuredAt := time.Unix(1795780000, 0)
	measurements := []claudeAccountMeasurement{
		{Account: "893ef8f2/0cea9af2", Label: "seth@x.io(Personal)", SubscriptionType: "max", Store: "keychain",
			MeasuredAt: measuredAt, FiveHour: map[string]any{"used_percentage": 7.0, "resets_at": 1795788000.0}},
		{Account: "893ef8f2/0cea9af2", Label: "seth@x.io(Personal)", SubscriptionType: "max", Store: "keychain",
			MeasuredAt: measuredAt.Add(time.Minute), FiveHour: map[string]any{"used_percentage": 8.0, "resets_at": 1795788000.0}},
		{Store: "keychain", MeasuredAt: measuredAt.Add(2 * time.Minute), UnmeasuredReason: "profile_failed", Detail: "/api/oauth/profile: HTTP 401"},
	}
	var logs []string
	next := 0
	reporter := newClaudeAccountReporter(func() (claudeAccountMeasurement, bool) {
		m := measurements[next]
		next++
		return m, true
	}, func(format string, a ...any) { logs = append(logs, strings.TrimSpace(fmt.Sprintf(format, a...))) })

	reporter.measureNow()
	wantFirst := map[string]any{
		"account": "893ef8f2/0cea9af2", "account_label": "seth@x.io(Personal)", "subscription_type": "max",
		"store": "keychain", "measured_at": 1795780000.0,
		"five_hour": map[string]any{"used_percentage": 7.0, "resets_at": 1795788000.0},
	}
	if got := reporter.snapshot(); !reflect.DeepEqual(got, wantFirst) {
		t.Errorf("snapshot = %v\nwant %v", got, wantFirst)
	}
	reporter.measureNow()
	reporter.measureNow()
	wantLast := map[string]any{"store": "keychain", "measured_at": 1795780120.0, "unmeasured_reason": "profile_failed"}
	if got := reporter.snapshot(); !reflect.DeepEqual(got, wantLast) {
		t.Errorf("snapshot after a failed profile = %v\nwant %v", got, wantLast)
	}
	wantLogs := []string{
		"claude account: 893ef8f2/0cea9af2 (from the keychain credential)",
		"claude account unmeasured: profile_failed (/api/oauth/profile: HTTP 401)",
	}
	if !reflect.DeepEqual(logs, wantLogs) {
		t.Errorf("logs = %q, want one line per change %q", logs, wantLogs)
	}

	t.Run("a host without claude reports nothing", func(t *testing.T) {
		r := newClaudeAccountReporter(func() (claudeAccountMeasurement, bool) { return claudeAccountMeasurement{}, false }, nil)
		r.measureNow()
		if got := r.snapshot(); got != nil {
			t.Errorf("snapshot = %v, want nil", got)
		}
	})
}

func TestClaudeAccountPoster(t *testing.T) {
	var seen []map[string]any
	post := claudeAccountPoster(func(path string, payload map[string]any) (int, map[string]any) {
		seen = append(seen, payload)
		return 200, nil
	}, func() map[string]any { return map[string]any{"account": "a/o", "measured_at": 1.0} })

	post(telemetryPath, map[string]any{"platform": "darwin/arm64"})
	post("/api/runtime-login", map[string]any{"state": "done"})

	want := []map[string]any{
		{"platform": "darwin/arm64", "claude_account": map[string]any{"account": "a/o", "measured_at": 1.0}},
		{"state": "done"},
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("posted %v, want %v", seen, want)
	}

	t.Run("a heartbeat built from any measurement carries no token, nor does the warden's log", func(t *testing.T) {
		now := time.Now()
		for name, stub := range map[string]*anthropicStub{
			"measured":       {profile: answer(personalProfile), usage: answer(personalUsage)},
			"profile failed": {profile: refuse(http.StatusUnauthorized), usage: answer(personalUsage)},
			"usage failed":   {profile: answer(personalProfile), usage: refuse(http.StatusInternalServerError)},
		} {
			t.Run(name, func(t *testing.T) {
				srv := httptest.NewServer(stub)
				defer srv.Close()
				home := t.TempDir()
				writeCredentialFile(t, filepath.Join(home, ".claude"), storedCredential(fileToken, "team", now.Add(time.Hour).UnixMilli()))
				runner := &wardenRunner{script: map[string]wardenRun{
					"security find-generic-password -a seth -s Claude Code-credentials -w": {out: storedCredential(keychainToken, "max", now.Add(time.Hour).UnixMilli())},
				}}
				meter := claudeAccountMeter{
					source:    claudeCredentialSource{goos: "darwin", home: claudeHome{Home: home}, user: "seth", readFile: os.ReadFile, runner: runner},
					installed: func() bool { return true },
					client:    srv.Client(), base: srv.URL, now: time.Now,
				}
				var logs []string
				reporter := newClaudeAccountReporter(meter.measure, func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) })
				var posted []byte
				post := claudeAccountPoster(func(path string, payload map[string]any) (int, map[string]any) {
					posted, _ = json.Marshal(payload)
					return 200, nil
				}, reporter.snapshot)

				reporter.measureNow()
				post(telemetryPath, map[string]any{})

				if !strings.Contains(string(posted), `"claude_account"`) {
					t.Fatalf("posted %s, want a claude_account block", posted)
				}
				for _, secret := range []string{keychainToken, fileToken, "sk-ant-ort01", "sk-ant"} {
					if strings.Contains(string(posted), secret) {
						t.Errorf("the heartbeat carried %q: %s", secret, posted)
					}
					for _, line := range logs {
						if strings.Contains(line, secret) {
							t.Errorf("the log carried %q: %s", secret, line)
						}
					}
				}
			})
		}
	})
}

func TestClaudeAccountReporterRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	measured := 0
	reporter := newClaudeAccountReporter(func() (claudeAccountMeasurement, bool) {
		measured++
		return claudeAccountMeasurement{UnmeasuredReason: "no_credential", MeasuredAt: time.Unix(1, 0)}, true
	}, nil)
	var waits []time.Duration
	reporter.run(ctx, func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		if len(waits) == 2 {
			cancel()
			return false
		}
		return true
	})
	if measured != 2 || !slices.Equal(waits, []time.Duration{claudeAccountInterval, claudeAccountInterval}) {
		t.Errorf("measured %d times with waits %v, want once at start and once per interval", measured, waits)
	}
}
