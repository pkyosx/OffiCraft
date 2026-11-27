package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const claudeAccountFileName = ".oc-claude-account"

// claudeLoginAccount is the account key and label exactly as cli/ocagent
// contextreport.go readClaudeAccount / readClaudeAccountLabel derive them: the
// station matches account cards, aliases and spend by this key, so a session
// bound here must name its account the way an unbound session's reports do.
type claudeLoginAccount struct {
	ID    string `json:"account"`
	Label string `json:"account_label,omitempty"`
}

// Real installs split fields across the two .claude.json files, so each field
// resolves independently, first file first.
func readClaudeLoginAccount(home string, readFile func(string) ([]byte, error)) claudeLoginAccount {
	if home == "" {
		return claudeLoginAccount{}
	}
	var accountUUID, userID, org, email, displayName, orgName string
	firstNonEmpty := func(dst *string, v any) {
		if s, ok := v.(string); ok && *dst == "" {
			*dst = strings.TrimSpace(s)
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", ".claude.json"),
		filepath.Join(home, ".claude.json"),
	} {
		raw, err := readFile(path)
		if err != nil {
			continue
		}
		var d map[string]any
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		if uid, ok := d["userID"]; ok && uid != nil && userID == "" {
			userID = strings.TrimSpace(fmt.Sprint(uid))
		}
		oauth, _ := d["oauthAccount"].(map[string]any)
		firstNonEmpty(&accountUUID, oauth["accountUuid"])
		firstNonEmpty(&org, oauth["organizationUuid"])
		firstNonEmpty(&email, oauth["emailAddress"])
		firstNonEmpty(&displayName, oauth["displayName"])
		firstNonEmpty(&orgName, oauth["organizationName"])
	}
	id := accountUUID
	if id == "" {
		id = userID
	}
	if id == "" {
		return claudeLoginAccount{}
	}
	if org != "" {
		id += "/" + org
	}
	label := email
	if label == "" {
		label = displayName
	}
	if label != "" && orgName != "" {
		label += "(" + orgName + ")"
	}
	return claudeLoginAccount{ID: id, Label: label}
}

// bindClaudeAccount records the login at launch as the account this claude
// session is billed to for its whole life: a running session keeps the
// credentials it started with until it is relaunched. The returned file is read
// into the launch environment; "" when no login is readable, and the member's
// reports then fall back to the machine's current login.
func (d SpawnDeps) bindClaudeAccount(memberID, workdir string) string {
	if d.ClaudeLogin == nil {
		return ""
	}
	path := filepath.Join(workdir, claudeAccountFileName)
	if err := d.Remove(path); err != nil && !os.IsNotExist(err) {
		d.logf("%s: could not clear the previous launch's Claude account (%v); launching unbound", memberID, err)
		return ""
	}
	account := d.ClaudeLogin()
	if account.ID == "" {
		d.logf("%s: no Claude login readable under the launch HOME; launching unbound", memberID)
		return ""
	}
	raw, err := json.Marshal(account)
	if err != nil {
		return ""
	}
	if err := d.WriteFile(path, string(raw), 0o600); err != nil {
		d.logf("%s: could not write the Claude account binding (%v); launching unbound", memberID, err)
		return ""
	}
	return path
}
