# cli/ocwarden P4 cutover runbook (python pull+telemetry → one golang push executor)

**Status: GATED. Do not run any step below until every Preflight item is checked.**

> ℹ️ **Historical port note (T-b461, 2026-07-21):** the `:8770` literals below are the
> dev port that was actually live *at cutover time* — this is a runbook of what was
> literally run/confirmed, not live config, so they are left as-is for historical
> accuracy rather than rewritten to the current default (`7755`,
> `server/ocserverd/config.go`). If this runbook is ever re-run against a fresh host,
> substitute the server's current default port for every `:8770` below.

This retires BOTH python warden daemons — the pull warden (`bin/reconcile-warden`
[no longer in-tree], launchd `com.officraft.warden`) AND the telemetry warden
(`bin/ocwarden` [no longer in-tree], launchd `com.officraft.telemetry`) — and puts
ONE golang stateless executor (`cli/ocwarden/`, the self-contained binary
`bin/ocwarden` run directly by launchd — the former `bin/warden-go` wrapper is folded
in — launchd `com.officraft.ocwarden`) in their place. Under Seth's model **one machine = one warden daemon = one identity/token**:
the golang binary is a single process with two loops — a command reader (hands:
spawn/stop/kill over the warden's authenticated SSE downstream) and a telemetry
reporter (eyes: POST telemetry + presence). The warden mints nothing and
self-bootstraps nothing: the server pushes each `member_token` down inside a
`warden-command` frame (Seth's model — see `CLAUDE.md §6`).

Direction is Seth-greenlit (push replaces pull, both python daemons retire). Execution
is still gated on: build-ready + CI green + independent reviewer clean + avo readiness
gate. No second Seth card is required.

---

## Preflight (all must be true before flipping)

- [ ] **be server producer half is live.** The producer is ALWAYS-ON (the
  `OC_SERVER_RECONCILE` toggle was retired — producer always-on is the only path).
  It goes live by ordinary code-land + autodeploy. Without the producer bound
  (`SseWardenDispatch.publish` → `WardenCommandQueue.enqueue`) the golang warden
  connects to `/api/events` but receives no commands — the flip is inert. Confirm
  `:8770 /api/version` git_sha is at/after the producer land AND the server log shows
  a reconcile cadence tick fire (mint + enqueue).
- [ ] **Execution-plane warden token provisioned.** Under Option A the golang warden
  REUSES the existing single warden identity for this host — member id
  `m-1a079734d735` (kind=="warden"), the same identity the telemetry warden used. Its
  token (scope=agent, sub=`m-1a079734d735`) is written to
  `~/.officraft/warden/exec-warden.tok` (mode `0600`). The server's `/api/events` handler
  authenticates the warden connection SOLELY by the token's verified `sub` — see
  `service/handlers.py:334-345`. `OC_ID` is server-side inert and the binary derives it
  from the token sub (`main.go:70-73`), so no separate id file is needed. There is NO
  second warden identity and NO second token: `make_host_of` (producer.py) resolves a
  host's frame to its single active kind=="warden" member deterministically because
  there is only one.
- [ ] **Binary builds clean at the landed sha:** `cd cli/ocwarden && go build -o
  ocwarden ./... && go vet ./... && go test -race ./...` all green; `gofmt -l .`
  empty.
- [ ] **This scaffold + the installer are landed on origin/main** (bin/ocwarden
  (committed binary) + the installer (flip-era bash `bin/warden-install`, since
  RETIRED/deleted — `ocwarden install` is the sole installer today) +
  deploy/com.officraft.ocwarden.plist + this runbook), dormant, reviewer-clean.
- [ ] **avo readiness gate passed.**

## Flip (NEVER `pkill`/pattern-kill — only launchctl bootout by exact label)

The golang job (`com.officraft.ocwarden`) and the python jobs
(`com.officraft.warden`, `com.officraft.telemetry`) run under DISTINCT labels, so
they COEXIST during cutover. Order is server-first (aligned with be): producer live →
golang proves it spawns → **only then** retire python. Nothing destructive touches the
python jobs until the golang path is verified live, so a golang failure leaves zero gap.

1. **be: server producer live.** Always-on via code-land + autodeploy (no host env
   flag). Confirm `:8770 /api/version` sha is at/after the producer land and the server
   log shows a reconcile cadence tick fire (mint + enqueue).
2. **Install + start the golang job via the one-key installer** — this is the
   go-forward warden install primitive; the flip dogfoods it (do NOT hand-`cp` the
   template plist — it carries `__ROOT__` placeholders and is un-installable as-is).
   avo mints the exec-warden token (`mint(member_id=m-1a079734d735, ttl_days=N)`) and
   writes it to `~/.officraft/warden/exec-warden.tok` (0600); then run, from the live repo:
   `OC_BASE=http://127.0.0.1:8770 OC_TOKEN="$(cat ~/.officraft/warden/exec-warden.tok)" bash bin/warden-install`
   (historical flip-time incantation — that bash installer has since been RETIRED/
   deleted; today's equivalent is `OC_BASE=… OC_TOKEN=… ./bin/ocwarden install`.)
   The installer builds `ocwarden`, (re)writes the tokfile 0600, renders + drops the
   plist (distinct label `com.officraft.ocwarden`, python jobs untouched),
   `launchctl bootstrap`s it, and **verifies the job is alive AND stable across a settle
   window** (a crash-loop under KeepAlive is caught, not false-greened). The command
   reader starts unconditionally once the warden has a real token/id (no toggle);
   `var/log/ocwarden.err.log` shows `command reader:
   enabled (SSE .../api/events)` and the server sees the warden SSE connection on
   sub=`m-1a079734d735` (the SOLE drainer — verified: no other connection holds
   /api/events on that sub).
3. **Verify the golang path spawns for real** (this is the acceptance test, not
   optional). The producer only enqueues a `start` when mira is observed-offline. With
   mira online-stable the queue stays empty — that proves connection, NOT spawn. So:
   **momentarily kill mira's tmux session once** → next producer tick (≤30s) sees mira
   offline → enqueues `start` to the `m-1a079734d735` queue → golang drains it and
   spawns `member-<id>` from the frame's member_token → agent boots and projects online
   (SSE presence). The python pull warden is STILL running through this window; the tmux
   clobber-guard makes the pull+push overlap idempotent (no double-spawn) and backs mira
   up = zero gap. Confirm from the golang log that it RECEIVED the start frame AND acted
   on it — connection alone is not proof.
4. **Only now retire BOTH python daemons** — the new path is proven, so tearing the old
   ones down leaves no gap. The golang binary subsumes both (pull + telemetry) under the
   same sub. No plist backup is kept (Seth: the plists are rebuildable from the repo,
   :8770 is gated dev). Bootout by EXACT label + remove the launchd plists + clean the
   python-era legacy files:
   `launchctl bootout gui/$(id -u)/com.officraft.warden`
   `launchctl bootout gui/$(id -u)/com.officraft.telemetry`
   `rm -f ~/Library/LaunchAgents/com.officraft.warden.plist ~/Library/LaunchAgents/com.officraft.telemetry.plist`
   Legacy files (per avo's cleanup list; KEEP `exec-warden.tok`):
   `rm -f ~/.officraft/telemetry-warden.tok` and any python-era
   `telemetry-launcher.sh` / `telemetry.*.log` left under `~/.officraft/`.
   Confirm gone: `launchctl list | grep -E 'com.officraft.(warden|telemetry)$'` →
   empty (only the `-go` job remains). NEVER `pkill`/pattern-kill — only launchctl
   bootout by exact captured label; never touch the `:8766` fleet.

## Verify (proves the executor actually enacts, not just connects)

- [ ] Server-driven **spawn**: momentary-kill mira → `start` frame → golang warden
  spawns `member-<id>` tmux session → the agent boots and projects online (SSE
  presence). Idempotent: a second `start` for an already-live member is absorbed by the
  spawn clobber-guard.
- [ ] Server-driven **stop**: a `stop` frame retires the session; `isMemberSession`
  gate refuses any non-member / telemetry-warden / bare `member-` target.
- [ ] Rolled into the **M1-complete e2e** (Joey), per Seth policy (RD self-tests land;
  Joey validates once at M1 completion).

## Rollback

Distinct labels + python-retired-LAST make rollback staged by where it fails:

- **Golang fails BEFORE step 4** (python still running): just remove the golang job.
  The python daemons never stopped → zero gap.
  `launchctl bootout gui/$(id -u)/com.officraft.ocwarden`
  `rm -f ~/Library/LaunchAgents/com.officraft.ocwarden.plist`
- **Golang misbehaves AFTER step 4** (python already retired): no backup was kept, so
  rebuild the python plists from the repo templates and re-bootstrap, then remove golang.
  The python plist templates live in the repo (`deploy/` / their launcher scripts); a
  fresh `launchctl bootstrap` of the rebuilt plists restores the python daemons. Then:
  `launchctl bootout gui/$(id -u)/com.officraft.ocwarden`.
  Confirm the python pull warden is back and spawning.

The labels never collide, so at no point do two jobs fight over one launchd identity;
the pull+push overlap in the coexistence window is made safe by the tmux clobber-guard
(spawn) and isMemberSession gate (kill), not by label exclusivity.

## Artifact → deploy (how the rebuilt binary reaches the running job)

The canonical DEPLOY binary is the committed `bin/ocwarden` (a stripped prebuilt,
`go build -ldflags="-s -w"`), which launchd execs DIRECTLY — the former `bin/warden-go`
wrapper is folded into it (the binary self-resolves the token via `OC_WARDEN_TOKFILE`).
Two mechanisms keep the deployed binary from drifting behind landed source (the "改了
golang 卻沒重編" 漏編 event):

1. **Compile gate (land-time).** `bin/ci.sh` gate 7 (`gofmt` + `go vet` + `go build`)
   is a HARD, every-run gate. Because ci.sh is the canonical autodeploy gate, no
   cli/ocwarden/*.go change can land or autodeploy while failing to compile. The staged
   `.githooks/pre-commit` mirror runs `go vet`+`go build` when a cli/ocwarden/*.go change
   is staged, catching it even earlier (convenience, not authority). Gate 7 builds a
   gitignored byproduct at `cli/ocwarden/ocwarden` purely to prove compilability; it is
   NOT the deploy artifact.

2. **Committed binary refresh.** The committed `bin/ocwarden` is the artifact the plist
   execs, so a golang source change must be accompanied by a rebuilt `bin/ocwarden`
   (`cd cli/ocwarden && go build -ldflags="-s -w" -o ../../bin/ocwarden .`). The flip-time
   bash installer rebuilt it in place (that installer is since retired — `ocwarden
   install` installs the committed prebuilt and never rebuilds); steady-state deploy
   must likewise refresh the committed binary and kickstart the job (below).

**Adoption (the one step ci.sh cannot do): restart the job.** A running launchd process
holds its old binary until restarted. So once the P4 flip installs
`com.officraft.ocwarden`, the autodeploy sequence must kickstart it AFTER ci.sh
rebuilds the binary — append to the deploy orchestration (the same place the server is
restarted):

    launchctl kickstart -k gui/$(id -u)/com.officraft.ocwarden

This is a **no-op until the flip** (the job does not exist pre-cutover — see Flip step 2),
which is why it lives here as the deploy contract rather than in a repo script that has no
job to kick yet. The flip-time installer (the retired bash `bin/warden-install`; today
`ocwarden install`) already bootstraps + settle-verifies; this kickstart is only the
steady-state per-deploy adoption after that.

## Post-cutover cleanup (once golang warden is proven in M1 e2e)

- Retire the python reconcile pull path (`agent/reconcile.py` self-bootstrap) — the
  dead pull warden logic. (`bin/reconcile-warden`, its launcher, is already no longer
  in-tree.)
- The python telemetry warden source (`bin/ocwarden`, already no longer in-tree) is
  fully subsumed by the golang warden's telemetry loop — nothing left to retire there.
- One machine now runs exactly one warden daemon (`com.officraft.ocwarden`) with one
  token (`exec-warden.tok`, shared by the SSE command reader and the telemetry POSTer).
