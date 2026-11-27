package main

import "ocserverd/txguard"

// Account usage as the machines measured it (claude_account on warden
// heartbeats), keyed by account. Volatile, like s.telemetry: it is the last
// measurement of each account, including accounts no machine is logged into
// any more, until a restart.
type accountQuotaStore struct {
	mu        txguard.Mutex
	byAccount map[string]accountQuota
}

type accountQuota struct {
	windows          map[string]any
	measuredAt       float64
	unmeasuredReason string
}

func newAccountQuotaStore() *accountQuotaStore {
	return &accountQuotaStore{byAccount: map[string]accountQuota{}}
}

// Record keeps an account's most recent measurement as it is. Every
// measurement of an account comes from that account's own credential, so
// there is nothing to merge.
func (st *accountQuotaStore) Record(account string, q accountQuota) {
	if account == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if prior, seen := st.byAccount[account]; seen && prior.measuredAt > q.measuredAt {
		return
	}
	st.byAccount[account] = q
}

func (st *accountQuotaStore) Snapshot() map[string]accountQuota {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]accountQuota, len(st.byAccount))
	for account, q := range st.byAccount {
		out[account] = q
	}
	return out
}
