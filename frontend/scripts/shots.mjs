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
  // f1 also shows 清除篩選: the default status set already narrows, so the
  // control is present from the first render (T-50bb + owner c-2423dba8b65b).
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

  // ── 傳承 (T-33) ───────────────────────────────────────────────────────────
  // 🔴 THE WHOLE POINT OF THESE FOUR. The 上線 gate on this ticket
  // (rc-df89e012e29d) says in as many words 「沒有人看過畫面」, and until now
  // that was structurally true rather than an oversight: the 傳承 page had ZERO
  // entries in this list, the visual guards mount single components, and the
  // one place a whole page could be photographed is this file. Nobody skipped a
  // step — there was no step.
  //
  // The 傳承 page needs no --seed hook: unlike replyCards/tasks, mock.ts ships
  // `mockLoreEntries` already populated, and those six rows deliberately cover
  // the branches the owner asked about — 三種 state, agent/manual/unknown 三種
  // 歸屬, and a DEPARTED author (m-gone) whose composer must not render.
  {
    name: "l1-lore-list",
    hash: "lore",
    // 這張沒有 act,所以它宣稱的是「預設進來就長這樣」——那也是一個宣稱。
    // 驗四個篩選鈕都在(少一個就不是 owner 裁的那一列了)、而且真的有列。
    // 空清單也會拍出一張很像樣的圖,只是那是 lore-empty,不是這張要的東西。
    verify: async (page) => {
      for (const id of [
        "lore-filter-author",
        "lore-filter-member",
        "lore-filter-belongs",
        "lore-filter-state",
      ]) {
        if (!(await page.locator(`[data-testid="${id}"]`).count())) {
          throw new Error(`篩選列少了 ${id}`);
        }
      }
      const rows = await page.locator('[data-testid="lore-row"]').count();
      if (rows < 3) throw new Error(`只有 ${rows} 列,拍到的可能是空狀態`);
    },
  },
  {
    // The four filters read 所有撰寫人 / 所有成員傳承 / 所有任務傳承 / 所有狀態
    // (owner ruling). A shot of the closed row shows the labels; this one shows
    // that the first of them actually opens onto real names.
    name: "l2-lore-filter-open",
    hash: "lore",
    act: async (page) => {
      await page.click('[data-testid="lore-filter-author"]');
      await page.waitForTimeout(300);
    },
    // 「開起來」才是這張的重點:關著的篩選列 l1 已經拍過了,再拍一次關著的
    // 不證明任何事。MultiSelectFilter 的每個選項是 `${testId}-opt-<value>`,
    // 所以數得到選項就代表選單真的展開、而且真的有人名可挑。
    verify: async (page) => {
      const opts = await page
        .locator('[data-testid^="lore-filter-author-opt-"]')
        .count();
      if (opts < 1) {
        throw new Error(`撰寫人選單數到 ${opts} 個選項,選單沒展開`);
      }
    },
  },
  {
    // ⚠️ THIS IS THE ONE THAT CAN LIE IF THE FIXTURE IS PLAIN TEXT. 本體是
    // markdown 算繪的,而 markdown 算繪壞掉的樣子是「標記語法原封不動印出來」——
    // 在一筆沒有任何標記的種子上,壞掉跟正常長得一模一樣。L-1 因此是唯一帶
    // markdown 的種子(見 mock.ts 上的紅字),它同時夠長,能讓摺疊真的摺起來。
    // 只有本體會摺,所以這張同時是「摺的是哪一段」的證據。
    name: "l3-lore-expanded-markdown",
    hash: "lore",
    act: async (page) => {
      await loreRow(page, "L-1").locator('[data-testid="lore-expand-mark"]').click();
      await page.waitForTimeout(400);
    },
    // 展開的必須是 L-1 那一列,而且 markdown 必須是**算繪後**的:
    // 算繪壞掉的樣子是把標記語法原樣印出來,所以看到字面上的 ** 或 `
    // 就是壞的。同時確認清單真的被算成 <li>,不是三行純文字。
    verify: async (page) => {
      const row = loreRow(page, "L-1");
      const body = row.locator('[data-testid="lore-body"]');
      const text = (await body.textContent()) ?? "";
      if (/\*\*|`/.test(text)) {
        throw new Error("markdown 沒有算繪:本體裡出現了字面上的標記符號");
      }
      const items = await body.locator("li").count();
      if (items !== 3) throw new Error(`預期 3 個 <li>,實際 ${items}`);
      if (!(await body.locator("strong").count())) {
        throw new Error("本體裡沒有任何 <strong>,粗體沒有算繪");
      }
    },
  },
  {
    // 離職的外包不該看到 composer。這張拍的是 L-3(authorId: m-gone),
    // 它同時是 retired 那一筆,所以退役理由也在同一張上。
    name: "l4-lore-departed-author",
    hash: "lore",
    act: async (page) => {
      // 🔁 這裡本來用 marks.nth(2) 抓「第三列」,拍到的是別筆,而照片看起來
      // 完全像對的。位置不是身分:列的順序由分組（置頂／生效中／退役）決定,
      // 而分組會隨資料變。所以改成按 id 定址。
      const row = loreRow(page, "L-3");
      await row.scrollIntoViewIfNeeded();
      await row.locator('[data-testid="lore-expand-mark"]').click();
      await page.waitForTimeout(400);
    },
    // 這張唯一要證的事就是「離職的撰寫人身上沒有輸入框」。
    verify: async (page) => {
      const row = loreRow(page, "L-3");
      if (!(await row.count())) throw new Error("L-3 不在畫面上");
      const composers = await row
        .locator('[data-testid="lore-author-composer"]')
        .count();
      if (composers !== 0) {
        throw new Error("離職撰寫人(m-gone)那一列仍然算繪了輸入框");
      }
    },
  },
];
// 傳承的列**按 id 定址,不按位置**。列的順序是分組出來的(置頂／生效中／退役),
// 所以 nth(n) 綁的是「目前資料剛好排成這樣」,不是那一筆條目 —— 換一筆種子的狀態,
// 同一個 nth 就靜靜地指向別人,而照片不會抗議。
const loreRow = (page, entryId) =>
  page.locator('[data-testid="lore-row"]').filter({
    has: page.locator('[data-testid="lore-entry-id"]', { hasText: entryId }),
  });

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
    executorKind: "staff",
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
    executorKind: "staff",
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
    executorKind: "staff",
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
      // 🔴 A SHOT THAT CANNOT PROVE ITS OWN CLAIM MUST FAIL, NOT DEVELOP.
      // The `applied:` line above only speaks about the FILTER FIELDS, so a
      // target whose claim is about anything else (which row is open, whether a
      // control is absent) got no check at all — and that is not hypothetical:
      // l4 shipped in its first run naming 「離職撰寫人」 while photographing a
      // different row, and the picture looked exactly like a pass, because
      // every state looks like a pass in a picture. `verify` is the target's
      // own assertion about the state it claims; throwing here loses the whole
      // run, which is the point — a wrong picture is worse than no picture,
      // since it is the one a reviewer will believe.
      if (target.verify) {
        try {
          await target.verify(page);
        } catch (e) {
          throw new Error(
            `[shots] ${target.name}: verify failed — the shot would have ` +
              `photographed a state it does not claim. ${e.message}`
          );
        }
      }
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
