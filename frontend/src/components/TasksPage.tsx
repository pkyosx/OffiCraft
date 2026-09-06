// TasksPage — the 任務 page (M3, SPEC §2): 篩選列 → 任務清單.
//
//   篩選列   — a FilterPanel (T-118). Four axes, ALWAYS VISIBLE, each taking
//             effect as soon as it is set. owner 2026-09-06 20:07
//             (c-c3d681fe05da):「不要多filter那一層了,全部拉出來」— so round 3's
//             funnel button, its 取消/套用篩選 pair, its 已篩選 strip and the
//             「案件」 sub-title above the list are all gone.
//             🔴 ONE axis still does not commit on keystroke: 任務編號 waits for
//             Enter or blur, because it changes a SERVER request (it is passed
//             to `useTasks` below, which reads `GET /api/tasks/{id}`). That is
//             the surviving half of 「每次都要全部都撈回來才濾不合理」 and the one
//             timing owner named this round. The three dropdowns commit at
//             once — a tick is already a whole condition.
//             狀態 is asked of the SERVER (T-a3e4: the fetch carries the applied
//             set, see the setStatuses effect); 負責人 / 類型 stay client-side
//             over the already status-narrowed list.
//   未結束  — every NON-terminal task in ONE list (狀態不分組 — the status
//             badge differentiates), ordered by priority 高→中→低→凍結 (凍結
//             永遠最後), createdTs newest-first within a level.
//   已結束  — 已完成 + 終止, COLLAPSED BY DEFAULT (the RepliesPage answered-
//             toggle pattern), newest close first. Both section titles carry
//             counts.
//
// Empty states ×2 (spec §2.3): no tasks at all vs filters matching nothing —
// plus the THREE by-id outcomes below, which replace both while an id is
// applied.
// The 30s ticking clock drives every card's 已歷時 / step 耗時 counters (same
// cadence as RepliesPage's 已等你).

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { TaskView } from "../api/adapter";
import { useTasks } from "../hooks/useTasks";
import { useTaskCount } from "../hooks/useTaskCount";
import { useMembers } from "../hooks/useMembers";
import { useHashRoute } from "../lib/hashRoute";
import { TaskCard } from "./TaskCard";
import { IdFilterInput } from "./IdFilterInput";
import { MultiSelectFilter, type MultiSelectOption } from "./MultiSelectFilter";
import { FilterPanel } from "./FilterPanel";
import { ChevronRightIcon } from "./icons";
import "./office.css"; // chat composer classes the embedded ReplyComposer reuses
import "./replies.css"; // shared reply-card interior styles (embedded cards)
import "./tasks.css";

const TERMINAL = new Set(["done", "terminated", "duplicated"]);

// 凍結永遠最後 (spec §2.2); an out-of-vocabulary priority sorts with low —
// never ahead of real high/mid, never displacing frozen's tail position.
const PRIORITY_RANK: Record<string, number> = {
  high: 0,
  mid: 1,
  low: 2,
  frozen: 4,
};

const STATUS_OPTIONS = [
  "not_started",
  "in_progress",
  "waiting_owner",
  "waiting_external",
  // "reassigning" is NO LONGER a status (T-9ca5) — it moved to the orthogonal
  // `task.lock`. It stays a 狀態-filter row for continuity, but its predicate
  // keys off task.lock (matchesStatus) and its label off lockReassigning.
  "reassigning",
  "done",
  "terminated",
  "duplicated",
];

// 一進頁面預設排除兩個終態 (done / terminated) — the status filter opens with
// every NON-terminal status checked, so the page shows only live tasks and the
// exclusion is visible (and undoable) right there in the 狀態 dropdown (T-be18).
const DEFAULT_STATUS = STATUS_OPTIONS.filter((s) => !TERMINAL.has(s));

export function TasksPage() {
  const { t } = useI18n();
  const { members } = useMembers();
  // ── 請示 → 任務: a reply card 查看任務詳情 routes to #tasks/<id>. That id
  // is just another filter dimension — the list narrows to that one task in the
  // normal layout, cleared the same way any other axis is: empty its field.
  // Read BEFORE useTasks so the anchored id reaches the hook in the SAME render
  // the hash lands in: routed through an effect instead, the mount fetch and the
  // page's empty-state decision would both run a commit before the hook knows
  // there is an anchor at all.
  const [route, setRoute] = useHashRoute();
  const taskIdFilter = route.page === "tasks" ? route.taskId : undefined;
  // The APPLIED id — declared HERE, above the hook, for the same reason the hash
  // is read above it: the by-id read must be in flight on the first render the
  // id exists in, or the page renders one commit's worth of 「找不到」 over a task
  // whose fetch has not been started yet.
  const [appliedId, setAppliedId] = useState(taskIdFilter ?? "");
  const {
    tasks,
    workers,
    taskTypes,
    loading,
    error,
    terminate,
    markDuplicate,
    setPriority,
    reassign,
    sendMessage,
    getDetail,
    removeArtifact,
    setStatuses,
    anchorPending,
    anchorFailed,
    // The hook's FIRST fetch must already carry the page's default set — the
    // mount fetch precedes any effect, and an unconstrained one would pull the
    // whole archive once per page open (T-a3e4).
    // The anchored id goes in as an ARGUMENT for the same reason: a jump landing
    // straight on #tasks/<id> must hydrate that one task from its own endpoint
    // on the very first pass, not one commit later.
  } = useTasks(DEFAULT_STATUS, appliedId === "" ? undefined : appliedId);

  // The unfiltered task total — the only honest basis for 目前沒有任務 now that
  // the list answers a status set (see the empty states below). Same cheap
  // count endpoint the nav badge rides, refetched on the same `task` deltas.
  const { total: taskTotal } = useTaskCount();

  // Ticking clock (30s) — drives 已歷時 and running-step 耗時 on every card.
  const [nowTs, setNowTs] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const timer = window.setInterval(
      () => setNowTs(Date.now() / 1000),
      30_000
    );
    return () => window.clearInterval(timer);
  }, []);

  // ── THE FOUR AXES (T-118) ────────────────────────────────────────────────
  // ONE copy of each axis now. Round 3 held two (applied / draft) because the
  // fields lived in a panel with 取消 / 套用篩選 at the bottom; owner 2026-09-06
  // 20:07 (c-c3d681fe05da) took the panel, both buttons and the draft away:
  //「不要多filter那一層了,全部拉出來」. A dropdown click is a complete condition,
  // so it takes effect at once and there is nothing left to stage.
  //
  // 🔴 THE ID IS THE ONE EXCEPTION, AND IT IS NOT AN OVERSIGHT. It keeps a
  // second piece of state (`draftId`, what is in the box) because a half-typed
  // id is not a condition — and because `useTasks(…, appliedId)` below turns an
  // applied id into a SERVER request, so committing per keystroke is a fetch
  // per keystroke. owner rejected that shape twice (「每次都要全部都撈回來才濾
  // 不合理」) and named the replacement this round:「按enter或是點外面就視為
  // apply」. `draftId` → `appliedId` happens on Enter and on blur, nowhere else.
  //
  // A dimension's Set holds the keys ticked; an EMPTY set means "no constraint"
  // (所有負責人 / 所有類型 / 所有狀態).
  //   executor keys: "outsource" | "unassigned" | <member id>
  //   type keys:     "adhoc" | <type_key>
  //   status keys:   <one of STATUS_OPTIONS>
  //
  // 狀態 opens at DEFAULT_STATUS — EXCEPT when the page opens on a #tasks/<id>
  // link. A jump means 「給我看這一張」, so it lands with no other axis narrowing
  // anything; seeded here in the initialiser rather than in the effect below so
  // the very first render already agrees with the fetch (an effect would let one
  // commit render the anchor against a status set the jump was about to drop —
  // and that commit is exactly where the 「條件不符」 notice would flash).
  const [appliedExecutor, setAppliedExecutor] = useState<Set<string>>(
    () => new Set()
  );
  const [appliedType, setAppliedType] = useState<Set<string>>(() => new Set());
  const [appliedStatus, setAppliedStatus] = useState<Set<string>>(() =>
    taskIdFilter ? new Set<string>() : new Set(DEFAULT_STATUS)
  );

  // What is IN THE BOX. `appliedId` above is what the page is filtered by; the
  // two differ exactly while the owner is mid-word.
  const [draftId, setDraftId] = useState(taskIdFilter ?? "");

  /** Enter or blur on the 任務編號 field — the only two events that turn typed
   * text into a filter (owner 2026-09-06:「按enter或是點外面就視為apply了」). */
  function commitId(next: string) {
    const nextId = next.trim();
    // Typing and then committing the SAME value must not re-run anything: this
    // fires on blur too, so tabbing through an untouched field would otherwise
    // repeat the by-id fetch for no reason.
    if (nextId === appliedId) {
      // Still normalise the box, so 「 t-1 」 settles to what is actually applied.
      if (nextId !== next) setDraftId(nextId);
      return;
    }
    setDraftId(nextId);
    setAppliedId(nextId);
    // The hash is a SEED, not a mirror: once the owner edits the id by hand the
    // URL must stop re-imposing the old one on the next render.
    if (taskIdFilter && nextId !== taskIdFilter) setRoute({ page: "tasks" });
  }

  // ── ID 篩選 — 全部條件一起生效 (owner 2026-09-06, option ①) ─────────────────
  // 🔴 The applied id does NOT filter the loaded list. It names ONE task, so the
  // page asks the server for exactly that one (`GET /api/tasks/{id}`, through
  // `useTasks`' anchor path) and then checks the answer against the OTHER
  // applied conditions. Round 2 filtered the already-loaded rows by substring
  // instead, and that is the defect this ticket exists for: 「不存在」 and
  // 「存在，只是沒被載進來」 rendered as the same sentence and the owner read a
  // present task as a deleted one in review.
  //
  // The three outcomes get three different answers on screen — see the by-id
  // block in the render, and `idOutcome` below.
  //
  // ⚠️ Fetching is why the field is EXACT-match now, where round 2 was a
  // substring: `GET /api/tasks/{id}` answers about one id. Nothing is asked
  // while the owner types — the fetch rides the APPLIED id, so a half-typed id
  // never reaches the wire.
  useEffect(() => {
    if (!taskIdFilter) return;
    setAppliedId(taskIdFilter);
    setDraftId(taskIdFilter);
    // 「跳到這一張」 — a link carries no opinion about 負責人/類型/狀態, so it
    // leaves none applied. Under 全部條件一起生效 this is what keeps a link to a
    // 已完成 task landing (T-4108 regression class): the anchor is not exempt
    // from the status axis, there simply is no status axis to fail.
    setAppliedExecutor(new Set());
    setAppliedType(new Set());
    setAppliedStatus(new Set());
  }, [taskIdFilter]);

  // ── 聊天 header 任務圖示 → #tasks/executor/<memberId> (T-dfae). Owner asked
  // for "that member's tasks that aren't done yet", so the seed sets BOTH axes
  // it promises rather than trusting the mount-time defaults: executor = that
  // member, status = the four non-terminal states, type = 所有類型. Doing it
  // explicitly matters — the page is NOT always a fresh mount (a stale
  // appliedStatus from an earlier visit would otherwise silently break the
  // "還沒完成" half of the promise, and a live type filter would hide rows).
  // It seeds the APPLIED axes (the panel's draft is reseeded from them the next
  // time it opens), and it clears any applied id: this jump is about a person,
  // not about one task.
  // One-shot (composeTaskNo precedent): the hash normalises back to #tasks the
  // moment it is consumed, so the seeded filters are ordinary, owner-editable
  // filter state — the fields edit them like any other.
  const executorSeed = route.page === "tasks" ? route.executorId : undefined;
  useEffect(() => {
    if (!executorSeed) return;
    setAppliedExecutor(new Set([executorSeed]));
    setAppliedType(new Set());
    setAppliedStatus(new Set(DEFAULT_STATUS));
    setAppliedId("");
    setRoute({ page: "tasks" });
  }, [executorSeed, setRoute]);

  // ── 勾什麼就問什麼 (T-a3e4) ────────────────────────────────────────────────
  // The fetch asks for the statuses the owner has APPLIED. ONE view genuinely
  // needs every status and says so by sending nothing: every 狀態 box unticked
  // (an empty set = 所有狀態) — there the owner asked for the whole population,
  // so downloading it is the answer, not a defect.
  //
  // 🔴 A by-id view is NOT such a view, and that is the 432 KB defect (owner
  // 2026-08-01): the id may name a task outside the ticked statuses, and the
  // page used to answer that by dropping the constraint and pulling the whole
  // history — 706 rows to make ONE card appear. `useTasks(…, appliedId)` reads
  // that single task from `GET /api/tasks/{id}` instead.
  // 🔴 …which is also why the effect SKIPS while an id is applied: with an id
  // on, the page renders that ONE fetched row and nothing else, so the list ask
  // is not on screen at all — re-asking it (with an empty set, i.e. the whole
  // archive) would be paying for rows nobody can see. The ask resumes the
  // moment the id comes off, which is the frame the list becomes visible again.
  //
  // What this REPLACED, and why the replacement is not just a rename: T-2b9d's
  // `open=true` fast path was switched off in practice by a fourth clause that
  // widened the fetch whenever ANY loaded task carried a dep — added by T-1d82
  // because a dep row had to resolve its title out of this very list, and a
  // closed dep was absent from the open-only list. With three live tasks
  // carrying deps that clause was always true, so every 任務 SSE delta
  // re-downloaded the entire history (measured: 408,482 B vs 17,295 B). The dep
  // rows now read their titles from the server's dep_tasks join, so the reason
  // for that clause is gone — deleted, not weakened. See TaskCard's dep block.
  // A joined key, not the Set: the Set is rebuilt on every render, so an effect
  // that depended on it would re-ask the server whenever anything else on the
  // page re-rendered (a 30s clock tick is enough).
  const statusAsk = [...appliedStatus].sort().join(",");
  useEffect(() => {
    if (appliedId !== "") return;
    setStatuses(statusAsk === "" ? [] : statusAsk.split(","));
  }, [statusAsk, appliedId, setStatuses]);

  // 🔴 WHERE 清除全部 WENT, AND WHY NOTHING REPLACED IT (T-118). Round 3 had a
  // 「清除全部」 button on the 已篩選 strip, and that strip was ALSO the
  // documented exit from a 404 anchor: an id that names nothing left the anchor
  // applied, and the button was how the owner got back to a list. owner
  // 2026-09-06 removed the strip (「也不用再顯示14筆已篩選跟那一行」), so both
  // went with it.
  //
  // The exit did NOT go with them, and that is the whole reason removing the
  // strip is safe: the 任務編號 box is now permanently on screen with the
  // offending id still in it. Emptying it and pressing Enter (or clicking away)
  // is the same escape, at the place the owner is already looking — which is
  // what 請示卡頁 has always done (docs/guide/interface.md). Each dropdown
  // clears the same way: untick its boxes.
  //
  // ⇒ If a future change hides the id field behind anything, it re-opens the
  // 404 trap this comment exists to record. The exit must stay reachable
  // WITHOUT the owner knowing the id was seeded from a hash.

  // Executor options: 外包 / 未指派 / 各成員 (real AI members only — machine-
  // layer wardens are not executors). An empty set = 所有人.
  const memberOptions = members.filter((m) => m.kind === "staff");
  // Type options: 各手冊類型 (the manuals list) ∪ any type present on a task
  // (covers a type whose manual was since deleted — closed tasks keep it).
  const typeOptions = [
    ...new Set([
      ...taskTypes.map((x) => x.typeKey),
      ...tasks.map((x) => x.typeKey).filter((k) => k !== ""),
    ]),
  ].sort();
  // type_key → display name (T-fa76): the filter labels and the cards' type
  // chips show the manual's human label; a deleted manual's key honestly
  // falls back to itself.
  const typeNames = new Map(
    taskTypes
      .filter((x) => x.displayName !== "")
      .map((x) => [x.typeKey, x.displayName])
  );

  // The executor key a task filters under (mirrors matchesExecutor's mapping).
  function executorKeyOf(task: TaskView): string {
    if (task.executorKind === "member") return task.executorId;
    return task.executorId === "" ? "unassigned" : "outsource";
  }

  // Per-dimension predicates, PARAMETERISED by the set they judge against — the
  // page now holds two copies of every axis (applied / draft) and both need the
  // same rules: the list is filtered by the APPLIED sets, while the 負責人
  // option counts describe the DRAFT the owner is currently editing.
  function passesExecutor(task: TaskView, set: Set<string>): boolean {
    return set.size === 0 || set.has(executorKeyOf(task));
  }
  function passesType(task: TaskView, set: Set<string>): boolean {
    const key = task.typeKey === "" ? "adhoc" : task.typeKey;
    return set.size === 0 || set.has(key);
  }
  function passesStatus(task: TaskView, set: Set<string>): boolean {
    if (set.size === 0) return true;
    if (set.has(task.status)) return true;
    // "reassigning" is an orthogonal LOCK, not a status (T-9ca5) — match it off
    // task.lock (a reassigned task still carries its honest derived status too).
    // 🔴 …but only while the task is OPEN, byte-for-byte the server's
    // taskStatusSetMatch rule (T-a3e4): terminate never clears the lock, so a
    // task terminated mid-handover keeps `lock="reassigning"` forever, and that
    // residue is not an intent. The three copies of this rule (server, here,
    // mock) must stay identical — a divergence means the list the server sent
    // and the list this page shows disagree about the same row.
    if (
      set.has("reassigning") &&
      task.lock === "reassigning" &&
      !TERMINAL.has(task.status)
    ) {
      return true;
    }
    return false;
  }
  const matchesExecutor = (task: TaskView) =>
    passesExecutor(task, appliedExecutor);
  const matchesType = (task: TaskView) => passesType(task, appliedType);
  const matchesStatus = (task: TaskView) => passesStatus(task, appliedStatus);
  // 全部條件一起生效 (owner ①) — the non-id axes AND together, and the id (when
  // one is applied) ANDs with them too, judged on the row the server returned
  // rather than on the loaded list. See `idOutcome`.
  const matchesOthers = (task: TaskView) =>
    matchesExecutor(task) && matchesType(task) && matchesStatus(task);

  // ── filter option models (labels + 負責人 counts) ──────────────────────────
  // Per-owner count basis (T-be18 #3): the tasks that WOULD show if this owner
  // were the sole executor pick — i.e. honouring the other live axes (status,
  // type) but not the executor axis itself. So the default count reads as
  // "active tasks on this person", and it moves in step with the status filter
  // (add 已完成 → counts grow). taskIdFilter is ignored here (a single-task
  // anchor isn't a status/type filter).
  // T-118: these used to read the DRAFT sets, because the counts sat inside a
  // panel and had to agree with the boxes next to them rather than with a list
  // the owner had not applied yet. There is no draft any more — a tick IS the
  // condition — so the applied sets are the only sets, and the counts and the
  // list are now incapable of disagreeing.
  const inCountScope = (task: TaskView) =>
    passesStatus(task, appliedStatus) && passesType(task, appliedType);
  const executorCount = (pred: (t: TaskView) => boolean) =>
    tasks.filter((t) => inCountScope(t) && pred(t)).length;
  const executorOptions: MultiSelectOption[] = [
    {
      value: "outsource",
      label: t.tasks.outsource,
      count: executorCount(
        (x) => x.executorKind === "outsource" && x.executorId !== ""
      ),
    },
    {
      value: "unassigned",
      label: t.tasks.unassigned,
      count: executorCount(
        (x) => x.executorKind === "outsource" && x.executorId === ""
      ),
    },
    ...memberOptions.map((m) => ({
      value: m.id,
      label: m.name,
      count: executorCount(
        (x) => x.executorKind === "member" && x.executorId === m.id
      ),
    })),
  ]
    // 負責人下拉只列在當前 status/type 結果集中有任務的執行者 (owner 回饋:計數
    // 0 者隱藏;外包與未指派同規則,T-be18 #3)。邊界:已勾選的執行者即使計數
    // 歸 0 也保留 — 否則使用者無法取消勾選、勾選態會卡死。
    .filter((o) => o.count > 0 || appliedExecutor.has(o.value));
  const typeFilterOptions: MultiSelectOption[] = [
    ...typeOptions.map((k) => ({ value: k, label: typeNames.get(k) ?? k })),
    { value: "adhoc", label: t.tasks.adhoc },
  ];
  const statusFilterOptions: MultiSelectOption[] = STATUS_OPTIONS.map((s) => ({
    value: s,
    // reassigning is a lock, not a status — its label lives under lockReassigning.
    label: s === "reassigning" ? t.tasks.lockReassigning : t.tasks.status[s],
  }));

  // ── the by-id outcome (owner 2026-09-06, option ①) ─────────────────────────
  // 🔴 THE THREE ENDINGS MUST NOT COLLAPSE INTO EACH OTHER. Round 2 filtered
  // the loaded list by substring, so 「這個編號不存在」 and 「它存在，只是不在這
  // 批載進來的列裡」 printed the SAME sentence — and that sentence fooled the
  // owner in review. With the id answered by `GET /api/tasks/{id}`, the page
  // knows which of the two it is and says so:
  //   "missing"  — the server answered 404. The id names nothing, today.
  //   "filtered" — the row came back, and it FAILS the other applied
  //                conditions. Named, with a one-press way to drop them.
  //   "match"    — it came back and passes: that one row is the list.
  //   "pending"  — the read has not landed. Say nothing at all yet.
  //   "unreached"— the read FAILED for a non-404 reason. The server was never
  //                reached, so 找不到 would be a lie about a question that was
  //                never answered.
  const idApplied = appliedId !== "";
  const idRow = idApplied
    ? tasks.find((x) => x.id === appliedId) ?? null
    : null;
  // FOUR outcomes, not the six of round 3. 「the server says it does not exist」
  // and 「it exists but another axis excludes it」 used to be separate because
  // each had its own notice on screen; owner 2026-09-06 removed both notices
  // (c-86c129855835:「不用 比數本來就是要顯示篩選過的數量」), so the two now
  // produce the identical screen — an empty result with the conditions in force
  // beside the count. Keeping the distinction here would be a branch nothing can
  // read. What still has to stay apart is 「we have an answer」 vs 「we do not」:
  // pending and unreached suppress the empty message, `empty` states it.
  const idOutcome: "none" | "pending" | "unreached" | "empty" | "match" =
    !idApplied
      ? "none"
      : anchorPending || loading
        ? "pending"
        : anchorFailed || error
          ? "unreached"
          : idRow !== null && matchesOthers(idRow)
            ? "match"
            : "empty";

  // 🔴 An applied id does NOT narrow the loaded list — it REPLACES it. The one
  // row it names either shows or is explained above; the rest of the list is
  // not "also matching", it is a different question.
  const filtered = idApplied
    ? idOutcome === "match" && idRow
      ? [idRow]
      : []
    : tasks.filter(matchesOthers);
  const open = filtered
    .filter((x) => !TERMINAL.has(x.status))
    .sort(
      (a, b) =>
        (PRIORITY_RANK[a.priority] ?? 2) - (PRIORITY_RANK[b.priority] ?? 2) ||
        b.createdTs - a.createdTs
    );
  const closed = filtered
    .filter((x) => TERMINAL.has(x.status))
    .sort((a, b) => (b.closedTs ?? 0) - (a.closedTs ?? 0));

  // 已結束 collapses by default (RepliesPage answered-toggle pattern): closed
  // tasks are reference material. Plain component state — never persisted.
  const [closedOpen, setClosedOpen] = useState(false);

  // A #tasks/<id> whose task does not exist USED TO self-heal: an effect here
  // stripped the hash and the page settled on the ordinary filtered list, with
  // nothing said. That is the defect owner 2026-09-05 ruled on (rc-428906235337,
  // 「這一包一起改」): a link that resolves to nothing and a link that was never
  // filtering look identical, so he cannot tell a broken link from a task that
  // is genuinely gone. The anchor now STAYS, `filtered` is empty, and the page
  // answers 沒有符合篩選條件的任務 — the same answer RepliesPage gives.
  //
  // 🔴 Removing the effect does not resurrect the flash it was written against:
  // the flash was the effect firing in the frames before the anchor's own fetch
  // landed, and `anchorPending` was the guard ON the effect, not a separate
  // mechanism. With no effect there is nothing to fire early. The other half of
  // its old job — the REJECTED-hydrate exit — is now served by the empty state:
  // `useTasks` resolves a failed anchor fetch WITH the id and a null task, so
  // `anchorPending` goes false and the page renders a message rather than a
  // stuck 載入中.
  //
  // 🔴 The way OUT is the 任務編號 field itself: empty it and press Enter (or
  // click away). T-118 removed the 清除篩選 button along with the 已篩選 strip it
  // stood on, and this comment used to name that button plus two helpers
  // (`clearFilters`, `anyFilter`) that no longer exist — the escape did not go
  // with them, it moved to the field, which is now permanently on screen
  // holding the offending id. That is why the anchor can stay without trapping
  // the owner in the hash.
  //
  // A closed target still auto-expands 已結束 so the one match is visible.
  // 🔴 Keyed on the APPLIED id, not the hash: an id TYPED into the field names a
  // closed task exactly as often as a link does, and if 已結束 stays collapsed
  // the page has "found and passes" as its verdict while showing no row at all —
  // a fourth, silent outcome, which is the failure mode this ticket is about.
  useEffect(() => {
    if (appliedId !== "") setClosedOpen(true);
  }, [appliedId]);

  // ── empty states, re-derived for the server-side ask (T-a3e4) ─────────────
  // 目前沒有任務 is a claim about the WHOLE workshop, and the list can no longer
  // support it: it answers only the ticked statuses, so zero rows equally means
  // 「什麼都沒有」 or 「這幾個狀態裡沒有」. The claim therefore rests on
  // `taskTotal` (GET /api/tasks/count's unfiltered total) — a grouped COUNT, NOT
  // a widened list fetch: wording a screen must never put the archive back on
  // the wire. Everything else falls to 沒有符合篩選條件的任務, which is true in
  // both worlds. Judging it from `tasks.length` alone (the pre-T-a3e4 rule) now
  // says 目前沒有任務 to an owner whose workshop is full of finished tasks.
  // `anchorPending` gates both: while the anchored task's own fetch is in flight
  // the filtered list is legitimately empty, and either message would be a claim
  // about a question that has not been answered yet.
  // 🔴 `anchorFailed` gates both for the same reason `anchorPending` does: it
  // means the anchored task's fetch never got an answer (a 500, an offline
  // browser), so BOTH messages would be claims about a question nobody asked.
  // A 404 is different — that IS an answer, and 沒有符合篩選條件的任務 is the
  // true thing to say about it. See useTasks' `anchorFailed`.
  // 🔴 …and BOTH are silent while an id is applied. That view has its own three
  // answers (see `idOutcome`), and the whole point of the ticket is that they
  // must not fall back into 沒有符合篩選條件的任務 — the sentence that made
  // 「不存在」 and 「在，只是沒被載進來」 look identical in the last round.
  const nothingAtAll =
    !idApplied &&
    !loading &&
    !error &&
    !anchorPending &&
    !anchorFailed &&
    tasks.length === 0 &&
    taskTotal === 0;
  // 🔴 AN APPLIED ID USES THE ORDINARY EMPTY STATE — owner 2026-09-06, four
  // messages on rc-f603bbd447f4 / c-2580b547d1a1 / c-a497d775aa4b /
  // c-86c129855835. Round 3 gave the by-id view two notices of its own (「找不到
  // 「X」」 and 「找到了，但不符合其他條件…只用編號再找一次」). He saw the first
  // one on the trial station and answered 「為什麼要顯示這種東西 拿掉!」, then
  // 「UI不是本來就秀0筆了嗎」, then 「任務那邊也可以用同樣的方式就好,不用特別再
  // 多個顯示框」.
  //
  // I put the second notice's case to him explicitly — a task that EXISTS but is
  // excluded by another axis would read as deleted, which is the confusion this
  // whole ticket was opened to end — and he overruled it: 「不用 比數本來就是要
  // 顯示篩選過的數量」. His model: the conditions in force are on screen in the
  // 已篩選 strip beside the count, so 0 筆 means "nothing matches what you asked
  // for", and the reader can see what he asked for. That is his call, recorded
  // here so the next person does not "restore" the notices as a bug fix.
  //
  // What that means mechanically: `idApplied` no longer suppresses this message.
  // The two states that must still stay silent are the ones where NOBODY HAS AN
  // ANSWER YET — the fetch is in flight (`pending`) or it never returned
  // (`unreached`) — because 沒有符合篩選條件的任務 would be a claim about a
  // question that was never answered. A 404 is an answer, so it says it.
  const nothingMatches =
    (!idApplied || (idOutcome !== "pending" && idOutcome !== "unreached")) &&
    !loading &&
    !error &&
    !anchorPending &&
    !anchorFailed &&
    !nothingAtAll &&
    filtered.length === 0;

  // ── 已篩選 chips: GONE (T-118) ────────────────────────────────────────────
  // owner 2026-09-06 (c-c3d681fe05da):「也不用再顯示14筆已篩選跟那一行」. The strip
  // existed because a COLLAPSED panel could narrow the list silently, and the
  // chips were the only place that said so. The panel does not collapse any
  // more — every field is on screen holding its own value — so the property the
  // chips protected is now carried by the fields themselves.
  //
  // ⇒ Re-adding a chip row would state each condition twice, in two places that
  // can drift. If a future change puts the fields back behind anything, the
  // chips have to come back WITH it; they are not independent decoration.

  function renderCard(task: TaskView) {
    return (
      <TaskCard
        key={task.id}
        task={task}
        allTasks={tasks}
        members={members}
        workers={workers}
        typeNames={typeNames}
        nowTs={nowTs}
        located={idApplied && task.id === appliedId}
        onTerminate={terminate}
        onMarkDuplicate={markDuplicate}
        onSetPriority={setPriority}
        onReassign={reassign}
        onSendMessage={(id, body, attachments) =>
          sendMessage(id, { body, attachments })
        }
        onHydrate={getDetail}
        onRemoveArtifact={removeArtifact}
      />
    );
  }

  return (
    <div className="tasks">
      {(error || anchorFailed) && (
        <div className="tasks__error" data-testid="tasks-error">
          {/* A failed by-id read is NOT 找不到 — it is 沒問到. Same box, and
            * deliberately different words: the owner must never read a broken
            * server as a deleted task. */}
          {anchorFailed ? t.tasks.idUnreached(appliedId) : t.tasks.loadError}
        </div>
      )}

      {/* ── 篩選面板 (T-93 round 3) — 「一起搬」: every axis lives in here ── */}
      {/* ── 篩選列 (T-118) — 四個軸,常駐可見,選了就生效 ── */}
      <FilterPanel testId="tasks-filter">
        {/* 10 characters: owner 2026-09-06 set this by hand — 任務 ids are not a
          * fixed length the way 請示卡 ids are (this station shows `T-93`; the
          * canonical form is `t-` + 12 hex), so there is no measurement to
          * derive it from and he picked one rather than have me invent it. */}
        <IdFilterInput
          value={draftId}
          onChange={setDraftId}
          onCommit={commitId}
          label={t.tasks.filterIdLabel}
          testId="filter-task-id"
          widthCh={10}
        />
        <MultiSelectFilter
          noun={t.tasks.filterExecutorNoun}
          allLabel={t.tasks.filterExecutorAll}
          options={executorOptions}
          selected={appliedExecutor}
          onChange={setAppliedExecutor}
          testId="filter-executor"
        />
        <MultiSelectFilter
          noun={t.tasks.filterTypeNoun}
          allLabel={t.tasks.filterTypeAll}
          options={typeFilterOptions}
          selected={appliedType}
          onChange={setAppliedType}
          testId="filter-type"
        />
        <MultiSelectFilter
          noun={t.tasks.filterStatusNoun}
          allLabel={t.tasks.filterStatusAll}
          options={statusFilterOptions}
          selected={appliedStatus}
          onChange={setAppliedStatus}
          testId="filter-status"
        />
      </FilterPanel>

      {/* ── empty states ×2 ── */}
      {nothingAtAll && (
        <div className="tasks__empty" data-testid="tasks-empty">
          {t.tasks.emptyNone}
        </div>
      )}
      {nothingMatches && (
        <div className="tasks__empty" data-testid="tasks-empty-filtered">
          {t.tasks.emptyFiltered}
        </div>
      )}

      {/* ── 未結束 ── */}
      {open.length > 0 && (
        <section className="tasks__section">
          <div className="tasks__section-title">
            {t.tasks.openTitle}
            {` · ${open.length}`}
          </div>
          <div className="tasks__list" data-testid="open-list">
            {open.map(renderCard)}
          </div>
        </section>
      )}

      {/* ── 已結束 (collapsible, default collapsed) ── */}
      {closed.length > 0 && (
        <section className="tasks__section">
          <button
            type="button"
            className="tasks__section-toggle"
            aria-expanded={closedOpen}
            data-testid="closed-toggle"
            onClick={() => setClosedOpen((v) => !v)}
          >
            <ChevronRightIcon
              size={13}
              className={`reply-card__caret${
                closedOpen ? " reply-card__caret--open" : ""
              }`}
            />
            {`${t.tasks.closedTitle} · ${closed.length}`}
          </button>
          {closedOpen && (
            <div className="tasks__list" data-testid="closed-list">
              {closed.map(renderCard)}
            </div>
          )}
        </section>
      )}
    </div>
  );
}
