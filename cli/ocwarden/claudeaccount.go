package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const claudeAccountFileName = ".oc-claude-account"

// ⚠️ The station's claudeAccountFreshSecs (server/ocserverd) is derived from
// this interval; raising it alone makes every account's pace verdict and the
// "still on its old account" note vanish between measurements.
const claudeAccountInterval = 5 * time.Minute

const (
	anthropicAPIBase   = "https://api.anthropic.com"
	claudeOAuthBeta    = "oauth-2025-04-20"
	claudeOAuthTimeout = 10 * time.Second

	anthropicProfilePath = "/api/oauth/profile"
	anthropicUsagePath   = "/api/oauth/usage"
)

// The reasons are a closed vocabulary the station validates.
const (
	claudeUnmeasuredNoCredential  = "no_credential"
	claudeUnmeasuredTokenExpired  = "token_expired"
	claudeUnmeasuredProfileFailed = "profile_failed"
	claudeUnmeasuredUsageFailed   = "usage_failed"
)

// claudeLoginAccount is what a claude spawn binds into the session's launch
// environment (OC_CLAUDE_ACCOUNT); cli/ocagent reads the same two keys.
type claudeLoginAccount struct {
	ID    string `json:"account"`
	Label string `json:"account_label,omitempty"`
}

// claudeCredential holds the live access token in memory for the two calls.
// ⚠️ Never format it into a log line, an error or a report: nothing in this
// file copies the token out of it, and the tests check the heartbeat and the
// log for it.
type claudeCredential struct {
	accessToken      string
	expiresAtMs      float64
	subscriptionType string
	store            string
}

// claudeCredentialSource reads the credential the way Claude Code does for a
// member this warden launches: same HOME, same config dir, same user, and the
// warden runs in the members' own (GUI) session, where the login keychain is
// readable. On macOS the keychain item wins over the plaintext file: a machine
// can hold both, for different logins, and members are billed to the keychain
// one.
// ⚠️ Claude Code reads the file only when the keychain answers nothing it can
// parse. A keychain item that parses but holds no login (its login moved to an
// encrypted overflow item, say) leaves the session without a usable login from
// the file either, so it must read as no credential here, not as the file's.
type claudeCredentialSource struct {
	goos     string
	home     claudeHome
	user     string
	readFile func(string) ([]byte, error)
	runner   CmdRunner
}

func (s claudeCredentialSource) configDir() string {
	if s.home.ConfigDir != "" {
		return s.home.ConfigDir
	}
	return filepath.Join(s.home.Home, ".claude")
}

// Claude Code suffixes the item with a hash of the config dir when
// CLAUDE_CONFIG_DIR is set, which the launch line does exactly when
// ConfigDir is.
func (s claudeCredentialSource) keychainService() string {
	service := claudeKeychainService
	if s.home.ConfigDir != "" {
		sum := sha256.Sum256([]byte(s.home.ConfigDir))
		service += "-" + hex.EncodeToString(sum[:])[:8]
	}
	return service
}

// read answers ok=false with cred.store naming the store that answered without
// a usable login, or "" when neither store answered.
func (s claudeCredentialSource) read() (claudeCredential, bool) {
	if s.home.Home == "" {
		return claudeCredential{}, false
	}
	if strings.HasPrefix(s.goos, "darwin") && s.runner != nil && s.user != "" {
		secret, err := s.runner.Run("security", "find-generic-password", "-a", s.user, "-s", s.keychainService(), "-w")
		if err == nil {
			if stored, parsed := parseClaudeStoredLogin([]byte(secret)); parsed {
				return stored.credential("keychain")
			}
		}
	}
	raw, err := s.readFile(filepath.Join(s.configDir(), ".credentials.json"))
	if err != nil {
		return claudeCredential{}, false
	}
	stored, parsed := parseClaudeStoredLogin(raw)
	if !parsed {
		return claudeCredential{}, false
	}
	return stored.credential("file")
}

type claudeStoredLogin struct {
	ClaudeAiOauth *struct {
		AccessToken      string  `json:"accessToken"`
		ExpiresAt        float64 `json:"expiresAt"`
		SubscriptionType string  `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// parsed is false for anything but a JSON object, as Claude Code's own parse.
func parseClaudeStoredLogin(raw []byte) (claudeStoredLogin, bool) {
	var stored *claudeStoredLogin
	if json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &stored) != nil || stored == nil {
		return claudeStoredLogin{}, false
	}
	return *stored, true
}

func (c claudeStoredLogin) credential(store string) (claudeCredential, bool) {
	oauth := c.ClaudeAiOauth
	if oauth == nil || strings.TrimSpace(oauth.AccessToken) == "" {
		return claudeCredential{store: store}, false
	}
	return claudeCredential{
		accessToken:      strings.TrimSpace(oauth.AccessToken),
		expiresAtMs:      oauth.ExpiresAt,
		subscriptionType: strings.TrimSpace(oauth.SubscriptionType),
		store:            store,
	}, true
}

// claudeAccountMeasurement is one answer from Anthropic about the login a
// member launched now would use. Detail is for the warden's own log only.
type claudeAccountMeasurement struct {
	Account          string
	Label            string
	SubscriptionType string
	Store            string
	MeasuredAt       time.Time
	FiveHour         map[string]any
	SevenDay         map[string]any
	UnmeasuredReason string
	Detail           string
}

func (m claudeAccountMeasurement) heartbeatReport() map[string]any {
	out := map[string]any{"measured_at": float64(m.MeasuredAt.UnixNano()) / 1e9}
	for key, value := range map[string]string{
		"account":           m.Account,
		"account_label":     m.Label,
		"subscription_type": m.SubscriptionType,
		"store":             m.Store,
		"unmeasured_reason": m.UnmeasuredReason,
	} {
		if value != "" {
			out[key] = value
		}
	}
	if m.FiveHour != nil {
		out["five_hour"] = m.FiveHour
	}
	if m.SevenDay != nil {
		out["seven_day"] = m.SevenDay
	}
	return out
}

func (m claudeAccountMeasurement) binding() claudeLoginAccount {
	return claudeLoginAccount{ID: m.Account, Label: m.Label}
}

type claudeAccountMeter struct {
	source    claudeCredentialSource
	installed func() bool
	client    *http.Client
	base      string
	now       func() time.Time
}

func newClaudeAccountMeter(env func(string) string, home claudeHome, runner CmdRunner, goos string) claudeAccountMeter {
	return claudeAccountMeter{
		source: claudeCredentialSource{
			goos:     goos,
			home:     home,
			user:     claudeKeychainUser(env),
			readFile: os.ReadFile,
			runner:   runner,
		},
		installed: func() bool { return resolveClaudeBin(env) != "" },
		client: &http.Client{
			Timeout: claudeOAuthTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		base: anthropicAPIBase,
		now:  time.Now,
	}
}

var claudeKeychainUserShape = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Claude Code names the keychain account after $USER, falling back to the OS
// user name, and to a fixed name when either has other characters.
func claudeKeychainUser(env func(string) string) string {
	name := strings.TrimSpace(env("USER"))
	if name == "" {
		if u, err := user.Current(); err == nil {
			name = u.Username
		}
	}
	if !claudeKeychainUserShape.MatchString(name) {
		return "claude-code-user"
	}
	return name
}

// measureLogin answers ok=false on a host without claude: there is no login to
// speak of, and "unknown" there would read as a fault.
func (m claudeAccountMeter) measureLogin() (claudeAccountMeasurement, bool) {
	if m.installed != nil && !m.installed() {
		return claudeAccountMeasurement{}, false
	}
	out := claudeAccountMeasurement{MeasuredAt: m.now()}
	cred, ok := m.source.read()
	if !ok {
		out.Store, out.UnmeasuredReason = cred.store, claudeUnmeasuredNoCredential
		if cred.store != "" {
			out.Detail = "the " + cred.store + " credential holds no Claude login access token"
		}
		return out, true
	}
	out.Store, out.SubscriptionType = cred.store, cred.subscriptionType
	if cred.expiresAtMs > 0 && float64(out.MeasuredAt.UnixMilli()) >= cred.expiresAtMs {
		out.UnmeasuredReason = claudeUnmeasuredTokenExpired
		return out, true
	}
	var profile struct {
		Account struct {
			UUID        string `json:"uuid"`
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if detail := m.anthropicGet(cred, anthropicProfilePath, &profile); detail != "" {
		out.UnmeasuredReason, out.Detail = claudeUnmeasuredProfileFailed, detail
		return out, true
	}
	account := strings.TrimSpace(profile.Account.UUID)
	if account == "" {
		out.UnmeasuredReason, out.Detail = claudeUnmeasuredProfileFailed, "profile has no account uuid"
		return out, true
	}
	if org := strings.TrimSpace(profile.Organization.UUID); org != "" {
		account += "/" + org
	}
	out.Account = account
	out.Label = claudeAccountLabel(profile.Account.Email, profile.Account.DisplayName, profile.Organization.Name)

	var usage map[string]json.RawMessage
	if detail := m.anthropicGet(cred, anthropicUsagePath, &usage); detail != "" {
		out.UnmeasuredReason, out.Detail = claudeUnmeasuredUsageFailed, detail
		return out, true
	}
	out.FiveHour = claudeUsageWindow(usage["five_hour"])
	out.SevenDay = claudeUsageWindow(usage["seven_day"])
	return out, true
}

// anthropicGet answers "" on success, else a description that never contains the
// token: only the path, the status, or the error class.
func (m claudeAccountMeter) anthropicGet(cred claudeCredential, path string, into any) string {
	ctx, cancel := context.WithTimeout(context.Background(), claudeOAuthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return path + ": cannot build the request"
	}
	req.Header.Set("Authorization", "Bearer "+cred.accessToken)
	req.Header.Set("anthropic-beta", claudeOAuthBeta)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := m.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return path + ": timed out"
		}
		return path + ": request failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("%s: HTTP %d", path, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || json.Unmarshal(raw, into) != nil {
		return path + ": unreadable answer"
	}
	return ""
}

func claudeAccountLabel(email, displayName, orgName string) string {
	base := strings.TrimSpace(email)
	if base == "" {
		base = strings.TrimSpace(displayName)
	}
	if base == "" {
		return ""
	}
	if org := strings.TrimSpace(orgName); org != "" {
		return base + "(" + org + ")"
	}
	return base
}

// The usage endpoint answers {utilization: percent|null, resets_at: ISO
// time|null}; the station reads windows shaped like a session's status-line
// rate_limits. A window without a number is left out, never zeroed.
func claudeUsageWindow(raw json.RawMessage) map[string]any {
	var window struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    *string  `json:"resets_at"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &window) != nil || window.Utilization == nil {
		return nil
	}
	out := map[string]any{"used_percentage": *window.Utilization}
	if window.ResetsAt != nil {
		if at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*window.ResetsAt)); err == nil {
			out["resets_at"] = float64(at.Unix())
		}
	}
	return out
}

// claudeAccountReporter keeps the latest measurement for the heartbeat; a
// spawn measures through it too, so the machine's reported login is the one
// the newest member was bound to. It also binds a claude launch that started
// unbound, once a measurement started after it names the account the machine
// was logged in as before it.
type claudeAccountReporter struct {
	meter func() (claudeAccountMeasurement, bool)
	logf  func(string, ...any)

	mu          sync.Mutex
	latest      map[string]any
	lastNote    string
	started     uint64
	lastAccount string

	// bindMu is held across a late binding's write, so a spawn's forget
	// returns only once no write for the previous launch can still land.
	bindMu  sync.Mutex
	unbound map[string]unboundClaudeLaunch
}

type unboundClaudeLaunch struct {
	path  string
	write func(path, content string, mode os.FileMode) error
	logf  func(string, ...any)
	// Set by awaitAccount: the last measurement started before the launch, and
	// the account the machine's last successful measurement before it named
	// ("" when none had).
	after        uint64
	priorAccount string
}

func newClaudeAccountReporter(meter func() (claudeAccountMeasurement, bool), logf func(string, ...any)) *claudeAccountReporter {
	return &claudeAccountReporter{meter: meter, logf: logf}
}

func (r *claudeAccountReporter) measureNow() claudeAccountMeasurement {
	if r == nil || r.meter == nil {
		return claudeAccountMeasurement{}
	}
	r.mu.Lock()
	r.started++
	seq := r.started
	r.mu.Unlock()
	m, ok := r.meter()
	note := ""
	switch {
	case !ok:
	case m.UnmeasuredReason == "":
		note = fmt.Sprintf("claude account: %s (from the %s credential)", m.Account, m.Store)
	default:
		note = fmt.Sprintf("claude account unmeasured: %s", m.UnmeasuredReason)
		if m.Account != "" {
			note += " for " + m.Account
		}
		if m.Detail != "" {
			note += " (" + m.Detail + ")"
		}
	}
	r.mu.Lock()
	if ok {
		r.latest = m.heartbeatReport()
	} else {
		r.latest = nil
	}
	if ok && m.Account != "" {
		r.lastAccount = m.Account
	}
	changed := note != r.lastNote
	r.lastNote = note
	r.mu.Unlock()
	if changed && note != "" && r.logf != nil {
		r.logf("%s", note)
	}
	if ok && m.Account != "" {
		r.bindUnbound(m, seq)
	}
	return m
}

// awaitAccount records a claude launch that started unbound.
func (r *claudeAccountReporter) awaitAccount(memberID string, launch unboundClaudeLaunch) {
	r.mu.Lock()
	launch.after, launch.priorAccount = r.started, r.lastAccount
	r.mu.Unlock()
	r.bindMu.Lock()
	defer r.bindMu.Unlock()
	if r.unbound == nil {
		r.unbound = map[string]unboundClaudeLaunch{}
	}
	r.unbound[memberID] = launch
}

// forget drops the member's previous launch, which a new spawn replaces.
func (r *claudeAccountReporter) forget(memberID string) {
	r.bindMu.Lock()
	defer r.bindMu.Unlock()
	delete(r.unbound, memberID)
}

// bindUnbound settles each pending launch with the first measurement started
// after it. A running claude keeps the login it started with, so the binding is
// written only when that measurement names the account measured before the
// launch; a login switched in between, or no earlier measurement, leaves the
// session unbound rather than naming an account it may not be using.
func (r *claudeAccountReporter) bindUnbound(m claudeAccountMeasurement, seq uint64) {
	r.bindMu.Lock()
	defer r.bindMu.Unlock()
	if len(r.unbound) == 0 {
		return
	}
	raw, err := json.Marshal(m.binding())
	if err != nil {
		return
	}
	for memberID, launch := range r.unbound {
		if seq <= launch.after {
			continue
		}
		delete(r.unbound, memberID)
		if launch.priorAccount == "" {
			launch.logf("%s: launched unbound with no earlier measurement of this machine's login; the session stays unbound", memberID)
			continue
		}
		if launch.priorAccount != m.Account {
			launch.logf("%s: launched unbound while this machine was logged in as %s, and it is now %s; the session stays unbound", memberID, launch.priorAccount, m.Account)
			continue
		}
		if err := launch.write(launch.path, string(raw), 0o600); err != nil {
			launch.logf("%s: could not write the Claude account binding (%v); the session stays unbound", memberID, err)
			continue
		}
		launch.logf("%s: launched unbound; now bound to %s, measured after it started (its earlier spend stays unattributed)", memberID, m.Account)
	}
}

func (r *claudeAccountReporter) snapshot() map[string]any {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest
}

func (r *claudeAccountReporter) run(ctx context.Context, sleep func(context.Context, time.Duration) bool) {
	for ctx.Err() == nil {
		r.measureNow()
		if !sleep(ctx, claudeAccountInterval) {
			return
		}
	}
}

// claudeAccountPoster adds the latest measurement to every heartbeat, so a
// restarted station has it again within one heartbeat.
func claudeAccountPoster(post Poster, snapshot func() map[string]any) Poster {
	return func(path string, payload map[string]any) (int, map[string]any) {
		if path == telemetryPath {
			if measured := snapshot(); measured != nil {
				payload["claude_account"] = measured
			}
		}
		return post(path, payload)
	}
}

// claudeAccountLaunch is what a claude launch line exports: File always when
// set, and the account read from it when the spawn bound one.
type claudeAccountLaunch struct {
	File  string
	Bound bool
}

// bindClaudeAccount records the account the profile endpoint named for the
// credential this claude session will read, as its account for its whole
// life: a running session keeps the credentials it started with until it is
// relaunched. When no account was identified the session launches unbound and
// the file may be written later (bindUnbound). A zero result means the session
// reports no account at all.
func (d SpawnDeps) bindClaudeAccount(memberID, workdir string, envPairs []agentEnvPair) claudeAccountLaunch {
	if d.ClaudeAccounts == nil {
		return claudeAccountLaunch{}
	}
	d.ClaudeAccounts.forget(memberID)
	path := filepath.Join(workdir, claudeAccountFileName)
	if err := d.Remove(path); err != nil && !os.IsNotExist(err) {
		d.logf("%s: could not clear the previous launch's Claude account (%v); launching unbound", memberID, err)
		return claudeAccountLaunch{}
	}
	if key := claudeCredentialEnvKey(envPairs); key != "" {
		d.logf("%s: the agent environment sets %s, which Claude Code uses instead of the stored login; launching unbound", memberID, key)
		return claudeAccountLaunch{}
	}
	m := d.ClaudeAccounts.measureNow()
	if m.Account == "" {
		reason := m.UnmeasuredReason
		if reason == "" {
			reason = "claude not installed"
		}
		d.logf("%s: no Claude account identified for the launch credential (%s); launching unbound until a measurement names one", memberID, reason)
		d.ClaudeAccounts.awaitAccount(memberID, unboundClaudeLaunch{path: path, write: d.WriteFile, logf: d.logf})
		return claudeAccountLaunch{File: path}
	}
	raw, err := json.Marshal(m.binding())
	if err != nil {
		return claudeAccountLaunch{}
	}
	if err := d.WriteFile(path, string(raw), 0o600); err != nil {
		d.logf("%s: could not write the Claude account binding (%v); launching unbound", memberID, err)
		return claudeAccountLaunch{}
	}
	return claudeAccountLaunch{File: path, Bound: true}
}

func claudeCredentialEnvKey(pairs []agentEnvPair) string {
	for _, pair := range pairs {
		for _, key := range claudeCredEnvKeys {
			if pair.Key == key && strings.TrimSpace(pair.Value) != "" {
				return key
			}
		}
	}
	return ""
}
