package main

import "ocserverd/txguard"

// The account rows fold over each member's CURRENT telemetry, so once a member
// re-tags to another account its last reading of the previous one is gone, and
// an account every live member has left falls back to whatever a stopped member
// froze, however old. This keeps each member's last reading per account and
// window until that window resets. Volatile, like s.telemetry.
type accountRateLimitStore struct {
	mu txguard.Mutex
	// account → agent → window key
	readings map[string]map[string]map[string]rateLimitCandidate
}

func newAccountRateLimitStore() *accountRateLimitStore {
	return &accountRateLimitStore{readings: map[string]map[string]map[string]rateLimitCandidate{}}
}

func (st *accountRateLimitStore) Record(agentID, account string, rateLimits map[string]any, measuredAt, now float64) {
	if account == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for windowKey, windowSecs := range WindowSeconds {
		window, resetAt, usable := usableRateLimitWindow(rateLimits[windowKey], windowSecs, now)
		if !usable {
			continue
		}
		if st.readings[account] == nil {
			st.readings[account] = map[string]map[string]rateLimitCandidate{}
		}
		if st.readings[account][agentID] == nil {
			st.readings[account][agentID] = map[string]rateLimitCandidate{}
		}
		st.readings[account][agentID][windowKey] = rateLimitCandidate{window: window, resetAt: resetAt, measuredAt: measuredAt}
	}
}

// LeftBehind answers, per account and window, the readings of members whose
// telemetry no longer names that account, dropping windows that have reset.
// A member still on the account is left out: its live entry already counts.
func (st *accountRateLimitStore) LeftBehind(telemetry map[string]map[string]any, now float64) map[string]map[string][]rateLimitCandidate {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string]map[string][]rateLimitCandidate{}
	for account, byAgent := range st.readings {
		for agentID, byWindow := range byAgent {
			for windowKey, c := range byWindow {
				if _, _, usable := usableRateLimitWindow(c.window, WindowSeconds[windowKey], now); !usable {
					delete(byWindow, windowKey)
				}
			}
			if len(byWindow) == 0 {
				delete(byAgent, agentID)
				continue
			}
			if current, _ := telemetry[agentID]["account"].(string); current == account {
				continue
			}
			if out[account] == nil {
				out[account] = map[string][]rateLimitCandidate{}
			}
			for windowKey, c := range byWindow {
				out[account][windowKey] = append(out[account][windowKey], c)
			}
		}
		if len(byAgent) == 0 {
			delete(st.readings, account)
		}
	}
	return out
}
