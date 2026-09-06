// shots.mjs — repeatable PNG capture of the running app in MOCK mode.
//
// WHY THIS EXISTS: the visual guards (visual-guards/*.ct.spec.tsx) mount single
// components and assert geometry; the paint guards sample frames of dist/.
// Neither one hands a human a picture of a WHOLE PAGE to look at. Reviewing a
// layout change ("does the filter bar crowd the list?") needs exactly that, and
// doing it by hand — start vite, guess the port, resize a window, crop — is not
// repeatable and not comparable across two branches.
//
// MOCK MODE, ALWAYS. VITE_USE_MOCK is left unset, which api/index.ts reads as
// mock (`import.meta.env.VITE_USE_MOCK !== "false"`), so no backend is needed
// and AuthGate never renders a wall (USE_MOCK short-circuits it to "app"). The
// script never invents a credential and never talks to a real server.
//
// THE SEED IS NOT DECORATION. api/mock.ts starts with `replyCards = []` and
// `tasks = []` on purpose — its honest hard line is that only a live agent
// creates one. So an unseeded #replies / #tasks shot is the EMPTY STATE, which
// is a real screen but not the screen anyone is reviewing when they change the
// list or its filters. seedMock() below reaches the mock's own test-only hooks
// (__injectMockReplyCard / __injectMockTask) through Vite's dev module graph —
// `import("/src/api/mock.ts")` in the page resolves to the SAME module instance
// the app imports, so the injected rows land in the store the UI is reading.
// Nothing under src/ is modified to make this work. Pass --no-seed for the
// genuine out-of-the-box empty state.
//
// THE PORT IS NOT PINNED, for the same reason paint-guards/freePort.ts is not:
// two working copies capturing shots at once must not fight over one number.
// Set SHOTS_BASE_URL to point at a dev server you started yourself and this
// script starts none.
//
// ADDING A SHOT IS ONE LINE in TARGETS.

import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { mkdir, rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { chromium } from "@playwright/test";

const ROOT = fileURLToPath(new URL("..", import.meta.url));

// ─── the shot list ────────────────────────────────────────────────────────────
// One line per picture. `hash` is the app's hash route WITHOUT the "#".
// A target may carry an `act(page)` that drives the UI before the shutter — the
// states worth reviewing on a SEARCH control are the ones that only exist after
// somebody typed and pressed, and a picture of the closed panel proves nothing
// about them.
const TARGETS = [
  // ── 篩選列 (T-118) ────────────────────────────────────────────────────────
  // The owner asked to SEE this before it lands (again — the same request that
  // created this file in round 2). What changed: there is no panel to open, so
  // the "open" shots are gone; what is worth photographing now is that the
  // fields are simply THERE, and that the id field still only takes effect on
  // Enter — a picture of a typed-but-uncommitted field next to an unchanged
  // list is the only way to SEE that timing at all.
  { name: "f1-tasks-row", hash: "tasks" },
  {
    name: "f2-tasks-typed-not-committed",
    hash: "tasks",
    // 🔴 THE SHOT THAT MATTERS. The box holds "T-93" and the list is still
    // whole, because nothing was pressed. If a future change wires the field to
    // onChange, THIS picture is the one that visibly breaks.
    act: async (page) => {
      await page.fill('[data-testid="filter-task-id"]', "T-93");
      await page.waitForTimeout(300);
    },
  },
  {
    name: "f3-tasks-committed-hit",
    hash: "tasks",
    act: async (page) => {
      await page.fill('[data-testid="filter-task-id"]', "T-93");
      await page.press('[data-testid="filter-task-id"]', "Enter");
      await page.waitForTimeout(400);
    },
  },
  {
    name: "f4-tasks-committed-miss",
    hash: "tasks",
    // Right SHAPE, nothing carries it. A malformed string would let a reader
    // dismiss the empty answer as "well, that is not an id"; the point of this
    // picture is that a well-formed id can be genuinely absent.
    act: async (page) => {
      await page.fill('[data-testid="filter-task-id"]', "T-9999");
      await page.press('[data-testid="filter-task-id"]', "Enter");
      await page.waitForTimeout(400);
    },
  },
  {
    name: "f5-tasks-dropdown-open",
    hash: "tasks",
    // The dropdowns apply on the click now, so this photographs the one moment
    // that still has a transient state worth seeing.
    act: async (page) => {
      await page.click('[data-testid="filter-status"]');
      await page.waitForTimeout(300);
    },
  },
  { name: "f6-replies-row", hash: "replies" },
  {
    name: "f7-replies-typed-not-committed",
    hash: "replies",
    act: async (page) => {
      await page.fill('[data-testid="filter-reply-card-id"]', "rc-428906235337");
      await page.waitForTimeout(300);
    },
  },
  {
    name: "f8-replies-committed-hit",
    hash: "replies",
    act: async (page) => {
      await page.fill('[data-testid="filter-reply-card-id"]', "rc-428906235337");
      await page.press('[data-testid="filter-reply-card-id"]', "Enter");
      await page.waitForTimeout(400);
    },
  },
];
const VIEWPORT = { width: 1440, height: 900 };
const DEVICE_SCALE_FACTOR = 2;

const args = process.argv.slice(2);
const SEED = !args.includes("--no-seed");
const OUT_DIR = path.resolve(
  ROOT,
  process.env.SHOTS_OUT ?? "shots-out"
);

// ─── the fixtures ─────────────────────────────────────────────────────────────
// Fixed ids and fixed prose, so the only thing that can move between a "before"
// and an "after" shot is the code under review. The clock anchor is Date.now()
// SNAPPED DOWN TO THE HOUR: every relative label the cards render ("已等你 25m",
// "已歷時 4h") is stable for anyone capturing both sides within the same hour,
// while still reading as a plausible recent time — a hard-coded epoch made the
// cards say "已等你 331d", which is a real render but not one anybody reviews.
const HOUR = 3600;
const NOW = Math.floor(Date.now() / 1000 / HOUR) * HOUR;

const REPLY_CARDS = [
  {
    id: "rc-428906235337",
    from: "mira",
    kind: "decision",
    summary: "要現在把這批更新同步到 Jira 嗎？",
    body: "同步會覆蓋 ACE-17658 的描述欄位。",
    options: [
      { text: "核可，直接同步上去", aiPick: true },
      { text: "先不要，我要再看一次", aiPick: false },
    ],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: NOW - 25 * 60,
    answeredTs: null,
    chatMessageId: "msg-9001",
    answer: null,
    task: null,
  },
  {
    id: "rc-77c1f0a2b8de",
    from: "mira",
    kind: "decision",
    summary: "這兩份設計稿要保留哪一份？",
    body: "",
    options: [
      { text: "保留 A 版", aiPick: false },
      { text: "保留 B 版", aiPick: true },
      { text: "兩份都留著", aiPick: false },
    ],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: NOW - 3 * HOUR,
    answeredTs: null,
    chatMessageId: "msg-9002",
    answer: null,
    task: null,
  },
  {
    id: "rc-1b93de40aa57",
    from: "mira",
    kind: "decision",
    summary: "報表要不要一起寄給財務？",
    body: "",
    options: [
      { text: "要", aiPick: true },
      { text: "不用", aiPick: false },
    ],
    selectMode: "single",
    status: "answered",
    attachments: [],
    createdTs: NOW - 26 * HOUR,
    answeredTs: NOW - 25 * HOUR,
    chatMessageId: "msg-9003",
    answer: { picks: ["要"], note: "" },
    task: null,
  },
];

const TASKS = [
  {
    id: "T-93",
    taskNo: "T-93",
    title: "等我回覆頁加上卡片編號篩選",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: NOW - 4 * HOUR,
    updatedTs: NOW - 12 * 60,
    closedTs: null,
    progressDone: 2,
    progressTotal: 5,
    steps: [],
  },
  {
    id: "T-94",
    taskNo: "T-94",
    title: "整理本週的出貨異常清單",
    typeKey: "",
    description: "",
    status: "waiting_external",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "等待倉庫回覆",
    duplicateOf: "",
    createdTs: NOW - 9 * HOUR,
    updatedTs: NOW - 2 * HOUR,
    closedTs: null,
    progressDone: 1,
    progressTotal: 4,
    steps: [],
  },
  {
    id: "T-88",
    taskNo: "T-88",
    title: "補上截圖工具的使用說明",
    typeKey: "",
    description: "",
    status: "done",
    priority: "low",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: NOW - 30 * HOUR,
    updatedTs: NOW - 26 * HOUR,
    closedTs: NOW - 26 * HOUR,
    progressDone: 3,
    progressTotal: 3,
    steps: [],
  },
];

// ─── plumbing ─────────────────────────────────────────────────────────────────

/** A port the OS reports as unused. Same probe shape as paint-guards/freePort.ts. */
function freePort() {
  const probe = createServer();
  try {
    probe.listen(0);
    const addr = probe.address();
    if (addr === null || typeof addr === "string") {
      throw new Error(
        "shots.mjs: node reported no bound address after listen(0). Start a dev " +
          "server yourself and set SHOTS_BASE_URL to work around this."
      );
    }
    return addr.port;
  } finally {
    probe.close();
  }
}

async function waitForServer(url, proc, timeoutMs = 120_000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    if (proc && proc.exitCode !== null) {
      throw new Error(`shots.mjs: dev server exited (code ${proc.exitCode}) before it served ${url}`);
    }
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    if (Date.now() > deadline) {
      throw new Error(`shots.mjs: dev server never answered ${url} within ${timeoutMs} ms`);
    }
    await new Promise((r) => setTimeout(r, 250));
  }
}

/**
 * Put deterministic rows into the mock's in-memory stores.
 *
 * Runs INSIDE the page: `import("/src/api/mock.ts")` under the vite dev server
 * hands back the very module object the app is already holding, so pushing
 * through its test-only hooks mutates the store the UI reads. The hooks also
 * emit their topic, so a page already mounted refreshes without a reload —
 * which matters, because a reload would throw the whole module graph away and
 * take the seed with it.
 */
async function seedMock(page) {
  const injected = await page.evaluate(
    async ({ cards, tasks }) => {
      const mock = await import("/src/api/mock.ts");
      if (typeof mock.__injectMockReplyCard !== "function" || typeof mock.__injectMockTask !== "function") {
        return { ok: false, reason: "mock module has no __injectMock* hooks" };
      }
      mock.__resetMock?.();
      for (const c of cards) mock.__injectMockReplyCard(c);
      for (const t of tasks) mock.__injectMockTask(t);
      return { ok: true };
    },
    { cards: REPLY_CARDS, tasks: TASKS }
  );
  if (!injected.ok) {
    throw new Error(`shots.mjs: could not seed the mock store — ${injected.reason}`);
  }
}

async function main() {
  let baseUrl = process.env.SHOTS_BASE_URL ?? "";
  let proc = null;

  if (!baseUrl) {
    const port = freePort();
    baseUrl = `http://localhost:${port}`;
    // VITE_USE_MOCK deliberately NOT set → api/index.ts defaults to the mock.
    proc = spawn("npm", ["run", "dev", "--", "--port", String(port), "--strictPort"], {
      cwd: ROOT,
      env: { ...process.env, VITE_USE_MOCK: "" },
      stdio: ["ignore", "pipe", "pipe"],
    });
    proc.stdout.on("data", (b) => process.stdout.write(`[vite] ${b}`));
    proc.stderr.on("data", (b) => process.stderr.write(`[vite] ${b}`));
    await waitForServer(`${baseUrl}/`, proc);
  } else {
    console.log(`[shots] reusing ${baseUrl} (SHOTS_BASE_URL)`);
  }

  await mkdir(OUT_DIR, { recursive: true });

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: DEVICE_SCALE_FACTOR,
  });
  const page = await context.newPage();
  const consoleErrors = [];
  page.on("console", (m) => {
    if (m.type() === "error") consoleErrors.push(m.text());
  });

  try {
    // Land on the root ONCE, seed, then move between shots by changing the hash
    // only — a hash change re-routes and re-reads the mock, a reload would wipe it.
    await page.goto(`${baseUrl}/`, { waitUntil: "load" });
    await page.waitForSelector("#root *", { timeout: 30_000 });
    if (SEED) await seedMock(page);

    let shotIndex = 0;
    for (const target of TARGETS) {
      await page.evaluate((h) => {
        window.location.hash = h;
      }, target.hash);
      await page.waitForFunction(
        (h) => window.location.hash.replace(/^#/, "") === h,
        target.hash
      );
      // Let the route's fetches land and any transition settle.
      await page.waitForTimeout(700);
      if (target.act) {
        // 🔴 ONLY BETWEEN shots — never before the first one. Shots share ONE
        // page, so whatever the previous act applied is still in force.
        //
        // 🔁 THIS USED TO PRESS 清除全部. That button was removed with the 已篩選
        // strip (owner 2026-09-06, c-c3d681fe05da), and its own comment here
        // recorded why pressing it was fragile anyway: it only EXISTED in some
        // of the designs under review, so the same script photographed two
        // different applied states depending on which one was loaded.
        // Clearing the FIELD is not variant-dependent in the same way — the
        // field is the design now — but the id is the only axis an act sets, so
        // that is all this has to undo.
        if (shotIndex > 0) {
        const field = page.locator(
          '[data-testid="filter-task-id"], [data-testid="filter-reply-card-id"]'
        ).first();
        if (await field.count()) {
          await field.fill("");
          await field.press("Enter");
        }
        // Close any dropdown the previous act left open.
        await page.keyboard.press("Escape");
        await page.waitForTimeout(400);
        }
        await target.act(page);
        await page.waitForTimeout(300);
      }
      // 🔴 SAY WHAT STATE WAS ACTUALLY PHOTOGRAPHED, for every shot. A picture
      // cannot tell you which state it caught — every state looks equally
      // plausible in one. This line is what caught two shots in this very file
      // that had photographed the WRONG state while looking exactly like a pass.
      // 🔁 WAS reading the 已篩選 strip. That strip is gone (T-118), and the
      // FIELD is now the thing that says what is applied — which is exactly the
      // property that made removing the strip safe, so reading it here is the
      // same check against the same fact.
      const summary = await page
        .locator(
          '[data-testid="filter-task-id"], [data-testid="filter-reply-card-id"]'
        )
        .first()
        .inputValue()
        .catch(() => null);
      const body = (await page.textContent(".app__main")) ?? "";
      const outcome = /找到「/.test(body)
        ? "filtered-out notice"
        : /找不到「/.test(body)
          ? "404 notice"
          : "rows";
      console.log(
        `[shots]   applied: ${
          summary ? summary.replace(/\s+/g, " ").trim() : "(none)"
        } | outcome: ${outcome}`
      );
      const file = path.join(OUT_DIR, `${target.name}.png`);
      await page.screenshot({ path: file });
      console.log(`[shots] ${target.name} → ${file}`);
      shotIndex += 1;
    }
  } finally {
    await context.close();
    await browser.close();
    if (proc) {
      proc.kill("SIGTERM");
    }
  }

  if (consoleErrors.length) {
    console.log(`[shots] ${consoleErrors.length} console error(s) during capture:`);
    for (const e of consoleErrors.slice(0, 10)) console.log(`  ! ${e}`);
  }
  console.log(`[shots] done — ${TARGETS.length} shot(s) in ${OUT_DIR}${SEED ? "" : " (unseeded / empty state)"}`);
}

main().then(
  () => process.exit(0),
  (err) => {
    console.error(err);
    process.exit(1);
  }
);
