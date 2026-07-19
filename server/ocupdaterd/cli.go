package main

// cli.go — the local management subcommands. They operate DIRECTLY on the
// database resolved from oc-updater.toml (no HTTP admin surface in M1): the
// operator runs them on the updater host itself. Safe to run while serve is
// up — both sides open the same SQLite file with a busy timeout (store.go).

import (
	"fmt"
	"io"
	"strconv"
	"time"
)

// withStore resolves config → store for one management op.
func withStore(env func(string) string, out io.Writer, op func(*Store) int) int {
	cfg, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
		return 1
	}
	store, err := openStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
		return 1
	}
	defer store.Close()
	return op(store)
}

// cmdMint mints one credential (publish token or invite) and prints the
// plaintext EXACTLY ONCE — only the hash is stored, so a lost token means
// minting a fresh one, never recovering the old.
func cmdMint(env func(string) string, kind, name string, out io.Writer) int {
	return withStore(env, out, func(store *Store) int {
		plaintext, secretHash, err := mintSecret(kind)
		if err != nil {
			fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
			return 1
		}
		id, err := store.InsertCredential(kind, name, secretHash)
		if err != nil {
			fmt.Fprintf(out, "[ocupdaterd] FATAL: store credential: %v\n", err)
			return 1
		}
		if kind == kindPublish {
			fmt.Fprintf(out, "publish token minted (id %d, name %q) — shown ONCE, store it now:\n%s\n", id, name, plaintext)
			fmt.Fprintln(out, "manage it later: ocupdaterd list-publish-tokens / revoke-publish-token <id>")
		} else {
			fmt.Fprintf(out, "invite minted (id %d) for %q — shown ONCE, hand it to them now:\n%s\n", id, name, plaintext)
		}
		return 0
	})
}

// credentialWords is the per-kind CLI vocabulary for the generic revoke/list
// commands: each management subcommand names ONE kind, and the store's kind
// gate guarantees it can never touch the other kind's rows.
type credentialWords struct {
	noun     string // "invite" / "publish token"
	article  string // "an" / "a" (for "%s %s id" phrasing)
	listCmd  string // the list subcommand to point the operator at
	mintHint string // what to run when the list is empty
}

func wordsFor(kind string) credentialWords {
	if kind == kindPublish {
		return credentialWords{
			noun:     "publish token",
			article:  "a",
			listCmd:  "list-publish-tokens",
			mintHint: "ocupdaterd mint-publish-token",
		}
	}
	return credentialWords{
		noun:     "invite",
		article:  "an",
		listCmd:  "list-invites",
		mintHint: "ocupdaterd mint-invite --name <who>",
	}
}

// cmdRevokeCredential revokes one credential of the named kind. The kind gate
// lives in the store (RevokeCredential) — an id of the OTHER kind is refused
// as not-found, so revoke-publish-token can never take down an invite.
func cmdRevokeCredential(env func(string) string, kind, arg string, out io.Writer) int {
	w := wordsFor(kind)
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		fmt.Fprintf(out, "[ocupdaterd] %q is not %s %s id (a number — find it with %s)\n", arg, w.article, w.noun, w.listCmd)
		return 2
	}
	return withStore(env, out, func(store *Store) int {
		ok, err := store.RevokeCredential(id, kind)
		if err != nil {
			fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(out, "[ocupdaterd] no live %s has id %d (already revoked, never existed, or a different credential kind — check %s)\n", w.noun, id, w.listCmd)
			return 1
		}
		fmt.Fprintf(out, "%s %d revoked — it stops working immediately\n", w.noun, id)
		return 0
	})
}

// cmdListCredentials lists every credential of the named kind (live and
// revoked), oldest first.
func cmdListCredentials(env func(string) string, kind string, out io.Writer) int {
	w := wordsFor(kind)
	return withStore(env, out, func(store *Store) int {
		creds, err := store.ListCredentials(kind)
		if err != nil {
			fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
			return 1
		}
		if len(creds) == 0 {
			fmt.Fprintf(out, "no %ss yet — mint one: %s\n", w.noun, w.mintHint)
			return 0
		}
		fmt.Fprintf(out, "%-6s %-20s %-20s %s\n", "ID", "NAME", "CREATED", "STATUS")
		for _, c := range creds {
			status := "live"
			if c.RevokedAt != nil {
				status = "revoked " + fmtUnix(*c.RevokedAt)
			}
			fmt.Fprintf(out, "%-6d %-20s %-20s %s\n", c.ID, c.Name, fmtUnix(c.CreatedAt), status)
		}
		return 0
	})
}

func cmdListVersions(env func(string) string, out io.Writer) int {
	return withStore(env, out, func(store *Store) int {
		releases, err := store.ListReleases()
		if err != nil {
			fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
			return 1
		}
		if len(releases) == 0 {
			fmt.Fprintln(out, "no versions published yet")
			return 0
		}
		fmt.Fprintf(out, "%-16s %-26s %-12s %-20s %-10s %s\n", "VERSION", "CHANNEL", "GIT_SHA", "PUBLISHED", "SIZE", "SHA256")
		for _, r := range releases {
			channel := "beta"
			if r.GAAt != nil {
				channel = "GA " + fmtUnix(*r.GAAt)
			}
			fmt.Fprintf(out, "%-16s %-26s %-12s %-20s %-10d %s\n", r.Version, channel, r.GitSHA, fmtUnix(r.PublishedAt), r.Size, r.SHA256)
		}
		return 0
	})
}

func fmtUnix(ts float64) string {
	return time.Unix(int64(ts), 0).UTC().Format("2006-01-02 15:04:05Z")
}
