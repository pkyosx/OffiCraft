// e2e_test/tests/05_machine_onboarding_spawn.spec.js
// C1 · machine onboarding → agent spawn → warden-log START (the "empty machine
// comes alive" chain, deterministic core).
//
// TARGET SHA: origin/main abfcd8f. This spec fixes the spike-validated chain into
// a rerunnable e2e: an owner onboards a fresh machine, a REAL `ocwarden run`
// (method A — `run`, NOT launchd `install`) brings it online over SSE, the owner
// hires an assistant and activates it onto that machine, and the warden spawns a
// REAL claude process inside a tmux session — with the START command_result
// folding back onto the member's last_op* (the warden→server observation seam).
//
// WHAT THIS DETERMINISTICALLY SEALS (坐實層次):
//   * ONBOARD contract — POST /api/machines mints machine_id (== member_id, the
//     warden member's own id) + a machine-bound exec token + the copy-paste
//     boot_command (curl /api/warden/binary → chmod → ocwarden install shape).
//   * MACHINE ONLINE — a real `ocwarden run` (env-fed OC_BASE/OC_TOKEN/OC_ID +
//     OC_CLAUDE_BIN) holds the /api/events SSE; the machine flips online<15s.
//   * ACTIVATE intent — POST /api/members/{id}/activate writes desired_state=online and
//     binds host == machine_id (owner INTENT; server can't reach the host).
//   * SPAWN (the load-bearing seam) — within ~60s a tmux session `member-<id>`
//     appears whose pane_pid is a REAL claude process launched with
//     --mcp-config + --model, pointed at THIS isolated server (verified by ps).
//   * WARDEN-LOG START — the warden's start command_result folds onto the member:
//     GET /api/members/{id} shows last_op=="start", last_op_ok==true, last_op_at>0.
//     (Step-4 of the reconcile RPC set; the observation channel per handlers.py
//     _fold_command_result → last_op*.)
//
// WHAT IS DELIBERATELY *NOT* HARD-ASSERTED (recorded via test.info only):
//   * agent online==true — the agent flips online only after IT mounts its own
//     ocagent SSE (waking→online is claude-driven and slow/flaky; the spike saw
//     it stall in waking). We seal the SPAWN + the server-observed START, not the
//     claude self-report.
//   * a STOP last_op — STOP is online-gated BY DESIGN: reconcile/machine.py:44-50
//     only dispatches the single robust stop when desired_state=offline ∧ STILL online;
//     an agent parked in waking never satisfies that gate, so a STOP receipt is
//     not deterministic. Teardown here is a precise tmux kill + DELETE, not a
//     reconcile-driven STOP, so no STOP assertion is made.
//
// ISOLATION / SAFETY: everything runs against the isolated :8791 server. The only
// tmux session touched is the one WE create (member-<our new id>); teardown kills
// exactly that session by name, kills exactly the warden PID we spawned, and
// DELETEs exactly the member + machine we created. The seed assistant (mira) and
// any ambient fleet warden session are never touched.
const { test, expect } = require('@playwright/test');
const { spawn, execSync } = require('node:child_process');
const path = require('node:path');
const fs = require('node:fs');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const PASSWORD = process.env.OC_E2E_PASSWORD || 'joey-e2e-local-pw';

// Repo layout: e2e_test/tests/05_*.spec.js → repo root is two dirs up; the built
// ocwarden binary lives beside the harness ("/tmp/joey-oc-harness/ocwarden" by
// default), overridable via OC_E2E_OCWARDEN.
const REPO_ROOT = path.resolve(__dirname, '..', '..');
const WARDEN_SRC = path.join(REPO_ROOT, 'cli', 'ocwarden');
const OCWARDEN =
  process.env.OC_E2E_OCWARDEN || path.resolve(REPO_ROOT, '..', 'ocwarden');
const GO_BIN = process.env.OC_E2E_GO || '/opt/homebrew/bin/go';

const MACHINE_LABEL = 'joey-e2e-m1';
const AGENT_NAME = 'joey-e2e-agent';
const MODEL = 'claude-opus-4-8';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function resolveClaudeBin() {
  if (process.env.OC_CLAUDE_BIN) return process.env.OC_CLAUDE_BIN;
  try {
    return execSync('command -v claude', { encoding: 'utf8' }).trim();
  } catch {
    return '';
  }
}

// Build ocwarden from cli/ocwarden (its OWN go.mod) if the binary is missing, so
// the spec is self-contained. Pure e2e concern — no production code touched.
function ensureOcwarden() {
  if (fs.existsSync(OCWARDEN)) return;
  execSync(`${GO_BIN} build -o ${OCWARDEN} .`, {
    cwd: WARDEN_SRC,
    stdio: 'inherit',
  });
}

test.describe('C1 · machine onboarding → agent spawn → warden-log START', () => {
  // Long chain: real warden boot + real claude spawn + reconcile ticks.
  test.setTimeout(180_000);

  test('an onboarded machine spawns an activated assistant and folds START', async ({
    request,
  }, testInfo) => {
    const claudeBin = resolveClaudeBin();
    expect(claudeBin, 'claude must be on PATH for the warden to spawn').toBeTruthy();
    ensureOcwarden();
    expect(fs.existsSync(OCWARDEN), `ocwarden binary must exist at ${OCWARDEN}`).toBe(
      true,
    );

    // ---- auth (owner) -----------------------------------------------------
    const login = await request.post(`${BASE}/api/login`, {
      data: { password: PASSWORD },
    });
    expect(login.status(), 'owner login must succeed').toBe(200);
    const { token: ownerTok } = await login.json();
    const auth = { Authorization: `Bearer ${ownerTok}` };

    // Resources to clean up in finally (declared up top so teardown always sees them).
    let machineId = '';
    let agentId = '';
    let wardenProc = null;
    let sessionName = '';

    try {
      // ---- STEP 1: onboard a fresh machine --------------------------------
      const onboard = await request.post(`${BASE}/api/machines`, {
        headers: auth,
        data: { display_name: MACHINE_LABEL },
      });
      expect(onboard.status(), 'machine onboard must succeed').toBe(200);
      const mBody = await onboard.json();
      machineId = mBody.machine_id;
      const machineTok = mBody.token;
      expect(machineId, 'machine_id must be minted (m- prefix)').toMatch(/^m-/);
      // The machine id IS the warden member's own id.
      expect(mBody.member_id).toBe(machineId);
      expect(machineTok, 'a machine-bound exec token must be minted').toBeTruthy();
      // boot_command carries the empty-machine installer one-liner contract:
      // the `curl .../install.sh?code=<one-time code> | bash` wrapper. The URL
      // carries the short-lived SINGLE-USE claim_code (TTL 600s) — never the
      // exec-token — and the served install.sh body redeems it via
      // POST /api/machines/claim before pulling the warden binary. The legacy
      // ?token= install path stays served byte-identical for old URLs.
      const boot = mBody.boot_command;
      expect(boot, 'boot_command must be a curl … | bash installer one-liner').toMatch(
        /^curl -fsSL .*\| bash$/,
      );
      expect(boot, 'boot_command must fetch the public /install.sh installer').toContain(
        '/install.sh?code=',
      );
      expect(mBody.claim_code, 'a one-time claim code must be minted').toBeTruthy();
      expect(mBody.claim_expires_in, 'claim code TTL must be 600s').toBe(600);
      expect(boot, 'boot_command must carry the one-time claim code').toContain(
        `code=${mBody.claim_code}`,
      );
      expect(boot, 'boot_command must never embed the exec-token').not.toContain(
        machineTok,
      );

      // ---- STEP 2: baseline — machine is offline before the warden runs ---
      const baseline = await (
        await request.get(`${BASE}/api/machines`, { headers: auth })
      ).json();
      const baseRow = baseline.find((m) => m.machine_id === machineId);
      expect(baseRow, 'the onboarded machine must appear on /api/machines').toBeTruthy();
      expect(baseRow.online, 'machine must be offline before the warden runs').toBe(
        false,
      );

      // ---- STEP 3: start `ocwarden run` (method A) → poll online<15s -------
      // Strip ambient fleet OC_* and pin the isolated triple + the claude bin so
      // the warden can genuinely spawn. `run` (not launchd install) holds the SSE.
      const wardenEnv = {
        ...process.env,
        OC_BASE: BASE,
        OC_TOKEN: machineTok,
        OC_ID: machineId,
        OC_CLAUDE_BIN: claudeBin,
      };
      delete wardenEnv.OC_WARDEN_TOKFILE; // force the explicit OC_TOKEN path.
      const wardenLogChunks = [];
      wardenProc = spawn(OCWARDEN, ['run'], { env: wardenEnv });
      wardenProc.stdout.on('data', (d) => wardenLogChunks.push(d.toString()));
      wardenProc.stderr.on('data', (d) => wardenLogChunks.push(d.toString()));

      let online = false;
      for (let i = 0; i < 15; i++) {
        await sleep(1000);
        const rows = await (
          await request.get(`${BASE}/api/machines`, { headers: auth })
        ).json();
        const row = rows.find((m) => m.machine_id === machineId);
        if (row && row.online === true) {
          online = true;
          testInfo.annotations.push({
            type: 'machine-online',
            description: `online after ~${i + 1}s`,
          });
          break;
        }
      }
      expect(online, 'machine must come online within 15s of `ocwarden run`').toBe(
        true,
      );

      // ---- STEP 4: hire an assistant --------------------------------------
      const hire = await request.post(`${BASE}/api/members`, {
        headers: auth,
        data: { name: AGENT_NAME, kind: 'assistant', model: MODEL },
      });
      expect(hire.status(), 'hire assistant must succeed').toBe(200);
      const hBody = await hire.json();
      agentId = hBody.id;
      expect(agentId, 'assistant id must be minted (m- prefix)').toMatch(/^m-/);
      expect(hBody.kind).toBe('assistant');

      // ---- STEP 5: activate the assistant onto the machine ----------------
      const activate = await request.post(
        `${BASE}/api/members/${agentId}/activate`,
        { headers: auth, data: { machine_id: machineId } },
      );
      expect(activate.status(), 'activate must succeed').toBe(200);
      const aBody = await activate.json();
      // Owner INTENT: desired_state flips online and the DESIRED placement binds to the
      // machine id. At 948c7d1 the placement field is `desired_machine_id` (the DESIRED
      // binding the warden reconciles against; handle_activate_member sets
      // m.desired_machine_id = body.machine_id). The old `host` field was removed —
      // `machine` on the DTO is the distinct OBSERVED position (empty until the warden
      // reports), NOT the intent written here.
      expect(aBody.desired_state, 'activate must write desired_state=online').toBe('online');
      expect(
        aBody.desired_machine_id,
        'activate must bind desired_machine_id (desired placement) to the machine id',
      ).toBe(machineId);

      // ---- STEP 6: the warden spawns a REAL claude in a tmux session ------
      sessionName = `member-${agentId.toLowerCase()}`;
      let panePid = '';
      for (let i = 0; i < 60; i++) {
        await sleep(1000);
        try {
          execSync(`tmux -L officraft has-session -t ${sessionName}`, {
            stdio: 'ignore',
          });
          panePid = execSync(
            `tmux -L officraft list-panes -t ${sessionName} -F '#{pane_pid}'`,
            { encoding: 'utf8' },
          )
            .trim()
            .split('\n')[0];
          if (panePid) {
            testInfo.annotations.push({
              type: 'spawn',
              description: `tmux session ${sessionName} after ~${i + 1}s pane_pid=${panePid}`,
            });
            break;
          }
        } catch {
          // session not there yet — keep polling.
        }
      }
      expect(
        panePid,
        `tmux session ${sessionName} must appear with a pane_pid within 60s`,
      ).toBeTruthy();

      // The pane process must be a REAL claude launched at THIS isolated server.
      const paneCmd = execSync(`ps -p ${panePid} -o command=`, {
        encoding: 'utf8',
      }).trim();
      expect(
        paneCmd,
        `pane pid ${panePid} must be a claude process, got: ${paneCmd}`,
      ).toMatch(/claude/);
      expect(
        paneCmd,
        'the spawned claude must carry an --mcp-config (isolated agent wiring)',
      ).toContain('mcp-config');

      // ---- STEP 7: warden-log START folds onto the member -----------------
      // The warden's start command_result rides back and _fold_command_result
      // writes it onto last_op* (server-observed START). The warden only fires the
      // receipt AFTER start() confirms a healthy spawn — measured ~40s after
      // activate / ~30s after the tmux session appears — so poll a generous ≤75s.
      let startFolded = false;
      let lastOp = null;
      for (let i = 0; i < 75; i++) {
        const m = await (
          await request.get(`${BASE}/api/members/${agentId}`, { headers: auth })
        ).json();
        lastOp = m;
        if (m.last_op === 'start' && m.last_op_ok === true && m.last_op_at) {
          startFolded = true;
          testInfo.annotations.push({
            type: 'warden-log-start',
            description: `last_op=start ok=true at=${m.last_op_at} after ~${i + 1}s`,
          });
          break;
        }
        await sleep(1000);
      }
      expect(
        startFolded,
        `START command_result must fold onto last_op*; last saw: last_op=${lastOp && lastOp.last_op} ok=${lastOp && lastOp.last_op_ok} at=${lastOp && lastOp.last_op_at}`,
      ).toBe(true);

      // ---- STEP 8: presence tri-state (waking SOFT, online HARD via poll) --
      // 948c7d1 rewired presence to SSE-first: the spawned REAL claude runs its
      // boot_sequence (report_waking → resume_summary → `ocagent listen`); only
      // when it mounts its own /api/events SSE does the hub flip is_online True and
      // on_first_connect clear waking_since → derived presence == "online"
      // (domain/member.py presence_state; realtime.py SSEHub.is_online; WAKING_TTL
      // 90s). We assert online via a GENEROUS poll (not a single shot): waking→online
      // is claude-driven with no firm upper bound (the spike often stalled in
      // waking), so a bounded poll+retry is the only non-flaky shape.
      //
      // waking is recorded SOFTLY (may flash past too fast to catch); online is the
      // load-bearing HARD assertion — the hole this spec previously left open.
      let sawWaking = false;
      let reachedOnline = false;
      let lastPresence = null;
      let onlineAfter = -1;
      const PRESENCE_POLL_TRIES = 40; // ~40 * 3s ≈ 120s (> WAKING_TTL 90s)
      // A transient ECONNREFUSED can hit mid-poll if serve briefly cycles its
      // listener (a graceful-shutdown window on the SSE hub). Playwright's
      // request.get THROWS on a refused socket, which would flake the whole run.
      // Absorb ONLY connection errors with a small BOUNDED inner retry — a genuine
      // down is NOT swallowed: exhausted retries count as a MISSED poll (not online),
      // and after the loop a final /api/version health re-probe distinguishes
      // "serve unreachable" (real down → FAIL naming it) from "up but never online".
      const CONN_RETRY = 3; // bounded: absorb a brief listener cycle, not a real down
      let lastPollErr = null;
      for (let i = 0; i < PRESENCE_POLL_TRIES; i++) {
        let m = null;
        for (let r = 0; r < CONN_RETRY; r++) {
          try {
            m = await (
              await request.get(`${BASE}/api/members/${agentId}`, { headers: auth })
            ).json();
            lastPollErr = null;
            break;
          } catch (err) {
            // connection-level error (ECONNREFUSED / socket hang up): transient
            // serve cycle. Brief backoff then retry; if all retries fail this poll
            // is MISSED (not treated as online) and the final probe judges it.
            lastPollErr = err && err.message ? err.message : String(err);
            await sleep(500);
          }
        }
        if (m) {
          lastPresence = m.presence;
          if (m.presence === 'waking') sawWaking = true;
          // online == (presence == "online") == hub.is_online (SSE mounted).
          if (m.presence === 'online') {
            reachedOnline = true;
            onlineAfter = (i + 1) * 3;
            break;
          }
        }
        await sleep(3000);
      }
      // SOFT: record whether we ever caught the transient waking window.
      testInfo.annotations.push({
        type: 'agent-presence-waking',
        description: `sawWaking=${sawWaking} (soft; waking window may flash past)`,
      });
      // FINAL HEALTH RE-PROBE — before declaring an online-timeout, confirm serve
      // is actually reachable. If /api/version itself is unreachable, the failure is
      // a SERVE-DOWN (named as such) — NOT a silently-swallowed online timeout.
      if (!reachedOnline) {
        let serveUp = false;
        for (let r = 0; r < 5; r++) {
          try {
            const v = await request.get(`${BASE}/api/version`, { headers: auth });
            if (v.ok()) {
              serveUp = true;
              break;
            }
          } catch (_) {
            /* still cycling — keep probing */
          }
          await sleep(1000);
        }
        expect(
          serveUp,
          `serve became UNREACHABLE during presence poll (final /api/version re-probe failed; lastPollErr=${lastPollErr}) — this is a serve-down, not an online timeout`,
        ).toBe(true);
      }
      // HARD: the agent must actually come online over its own SSE.
      expect(
        reachedOnline,
        `agent must reach presence=online within ~120s (SSE-mounted); last presence seen: ${lastPresence}`,
      ).toBe(true);
      testInfo.annotations.push({
        type: 'agent-presence-online',
        description: `presence=online after ~${onlineAfter}s (SSE-first, waking cleared)`,
      });

      // ---- STEP 9: ZERO-FRICTION scan (core acceptance for 948c7d1) --------
      // The caller-identity rewrite (report_waking/stopping/stopped carry NO
      // member_id — caller resolved from JWT sub; migration 0024 dropped
      // session_id/pid/last_alive) means a freshly-onboarded agent should NOT have
      // to figure out its own id or self-repair its env. We read the agent's onboard
      // chatter (owner↔agent stream) and assert ZERO self-rescue / "can't find my
      // own id" signals. Any hit is a FAIL and the offending message is printed.
      // (Keyword set ported from single_machine_e2e.sh scan_self_rescue, plus the
      // identity-specific "member_id / 找不到自己 id" signatures this change targets.)
      const FRICTION_RX =
        /exit 127|command not found|no such file|找不到|找不到自己|member_id|stale|我來修|patch|死路徑|who am i|my own id|can'?t find my id/i;
      const chatRes = await request.get(
        `${BASE}/api/chat?with=${agentId}&limit=500`,
        { headers: auth },
      );
      expect(chatRes.status(), 'chat read must succeed').toBe(200);
      const chat = await chatRes.json();
      const frictionHits = (Array.isArray(chat) ? chat : [])
        .filter((msg) => msg && typeof msg.body === 'string' && FRICTION_RX.test(msg.body))
        .map((msg) => `ts=${msg.ts} ${msg.from}->${msg.to}: ${msg.body}`);
      testInfo.annotations.push({
        type: 'friction-scan',
        description: `scanned ${Array.isArray(chat) ? chat.length : 0} msg(s), ${frictionHits.length} friction hit(s)`,
      });
      expect(
        frictionHits.length,
        `zero-friction onboard expected (0 self-rescue hits); got:\n${frictionHits.join('\n')}`,
      ).toBe(0);
    } finally {
      // ---- PRECISE TEARDOWN (isolation-safe) ------------------------------
      // 1. kill EXACTLY our tmux session (never kill-server / never mira / never
      //    an ambient fleet warden session).
      if (sessionName) {
        try {
          execSync(`tmux -L officraft kill-session -t ${sessionName}`, {
            stdio: 'ignore',
          });
        } catch {
          /* already gone */
        }
      }
      // 2. kill EXACTLY the warden PID we spawned.
      if (wardenProc && wardenProc.pid) {
        try {
          wardenProc.kill('SIGTERM');
        } catch {
          /* already gone */
        }
      }
      await sleep(1500);
      // 3. DELETE the member + machine we created (best-effort, idempotent).
      if (agentId) {
        try {
          await request.delete(`${BASE}/api/members/${agentId}`, { headers: auth });
        } catch {
          /* ignore */
        }
      }
      if (machineId) {
        try {
          await request.delete(`${BASE}/api/machines/${machineId}`, {
            headers: auth,
          });
        } catch {
          /* ignore */
        }
      }
    }
  });
});
