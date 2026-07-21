# officraft — E2E Smoke Suite

A **big-version smoke gate** for officraft: a growable end-to-end harness with
the shape **`setup → e2e × N → teardown`**. One `setup` brings up an isolated
service, N independent Playwright specs exercise it, one `teardown` returns the
machine to a clean slate. Re-run it after any large change to confirm nothing
regressed.

## Why this exists

A batch of cockpit **display regressions** shipped unnoticed — raw ids instead of
friendly names (account / machine columns), an empty model column, CPU/RAM/POWER
metric cards vanishing, duplicated presence text, chat not auto-scrolling. Those
are mostly **statically checkable** and should have been caught by an assertion.
This suite closes that coverage gap and makes "does the big version still work?"
a one-command, objectively-verified check.

## Layout

```
e2e_test/
  setup.sh              # start isolated service: fresh DB → migrate → serve(:8791) → login
  teardown.sh           # tear down: stop serve (only our pid) → drop isolated DB → verify
  run_all.sh            # one-shot: setup → playwright specs → teardown (teardown always runs)
  playwright.config.js
  package.json          # @playwright/test
  lib/common.sh         # shared config + prod-safety guards
  tests/
    01_login.spec.js                   # A · skeleton: version up, login, token authorizes DB-backed endpoint
    02_monitoring_hardware_cards.spec.js  # B1 · monitor page renders CPU/RAM/POWER columns (browser)
    03_chat_autoscroll.spec.js         # B6 · chat thread autoscrolls to newest (browser)
    04_session_display_names.spec.js   # B2/B3 · session row resolves machine/account to friendly names (API)
    …                                  # add scenarios here, one file per slice
```

### Display-regression specs (B group) & the `OC_E2E_BASELINE` switch

The B specs guard the cockpit display regressions Seth hit. Each is written as the
**correct expectation** (the column exists, the thread is scrolled to bottom, the
session shows friendly names) and doubles as a red→green harness:

- Run normally → the assertion must genuinely **pass** (permanent green regression
  guard on the fixed build).
- Run with `OC_E2E_BASELINE=1` against a *pre-fix* build → the assertion is expected
  to fail, and `test.fail()` flips that into the pass condition, so the run
  documents the **baseline red**. This is how each fix was independently verified
  red (pre-fix) → green (post-fix) without trusting a self-report.

`setup.sh` builds the SPA (`VITE_USE_MOCK=false`) so the browser specs (02/03) have
a mounted cockpit; `run_all.sh` installs Chromium. API-only specs (01/04) don't need
either — set `OC_E2E_SKIP_BUILD=1` to skip the SPA build when running only those.

## Run

```bash
cd e2e_test
bash run_all.sh          # setup → all specs → teardown, in one shot
```

### Target

The suite runs against the Go server (`ocserverd`) — the only implementation
since the Python backend retired (rollback anchor: git tag `py-final`):

```bash
bash run_all.sh                    # stage SPA → go build ocserverd (go:embed webdist)
OC_E2E_TARGET=go bash run_all.sh   #     → ocserverd migrate (goose) → ocserverd serve
```

Port :8791, repo-root `oc.toml`, fresh-DB lifecycle, exact-pid teardown. The
run builds `ocserverd` fresh into `.state/` with the SPA staged into
`server/ocserverd/webdist/` first, so the browser specs get the embedded
cockpit.

Or drive the phases by hand:

```bash
bash setup.sh            # bring the service up, persist token to .state/
OC_E2E_BASE=http://127.0.0.1:8791 npx playwright test
bash teardown.sh         # clean up
```

## Isolation & prod safety (hard rules)

- Runs on a **non-prod port (:8791)** with an **isolated SQLite** DB. `common.sh`
  **hard-refuses** to run against a prod port. The officraft one is **derived at
  run time** from `server/ocserverd/config.go`'s `defaultPort` (7755 today) —
  *not* a number hand-copied into the harness; an unparseable config.go is a
  FATAL exit 2, never a silently-skipped guard. On top of that it refuses the
  hand-maintained set that nothing in this repo can derive: **8770 / 8780**
  (officraft's own *retired* former defaults — see config.go's migration
  history — kept for installs that still pin one in `oc.toml`) and **8766**
  (a *different* product's live port). Earlier revisions of this line said
  "prod ports :8770 / :8766", which named only retired/foreign ports and never
  the actual current one (T-a3ba).
- Ambient fleet env (`OC_ID` / `OC_TOKEN` / `OC_BASE`) is **stripped** before
  starting the service or any tool, so nothing authenticates against or emits to
  the fleet/prod server.
- `teardown.sh` stops **only the pid it captured** at setup — never `pkill` /
  `killall`.

## Scope

The **Playwright suite** (`setup.sh` / `run_all.sh` / `tests/`) covers
single-machine scenarios on the isolated :8791 server. A **single warden**
running correctly (onboarding / install / operation) **is** in scope there.

Anything that needs a **real second machine** (relocate between machines,
cross-machine agent migration) lives in the separate **`cross_machine.sh`**
script below — NOT in the Playwright suite.

## cross_machine.sh — DESTRUCTIVE multi-machine full-reset regression

`cross_machine.sh` is a MANUAL, **DESTRUCTIVE** end-to-end regression that runs
against the CANONICAL server layout (not :8791) — note :8770 here is the retired
former prod port that `cross_machine.sh` still hard-codes as its serve/public
port, which no longer matches `oc_lifecycle.sh`'s canonical port (:7755, read
from config.go); tracked separately: it tears down and
re-installs the local server + wardens from zero, spawns the seed agent,
onboards a REAL second machine over ssh (default `eva-m5`), relocates the agent
there, and asserts zero self-repair before AND after the move. It wipes the
local `~/.officraft` server state (DB backed up to /tmp first) and rotates
the owner password to a fresh uuid, so it is run BY HAND by an operator who
knows exactly that:

```
OC_CROSS_MACHINE_YES=1 REQUIRE_ISOLATION_CONFIRMED=1 bash e2e_test/cross_machine.sh
```

Both env acks are REQUIRED (destructive ack + warden-isolation ack — read the
script header before the first run on a new box). It is intentionally NOT wired
into `run_all.sh`. Params (PUBLIC_HOST / SECOND_MACHINE / TEST_AGENT / …) are
documented in the script header.

## Adding a scenario

Drop a new `NN_<name>.spec.js` under `tests/`. Prefer `APIRequestContext` for
data/contract checks; use a real `page` (browser) for render/display assertions
(those need `npx playwright install chromium`). Keep each spec independent so one
failure doesn't block the rest.
