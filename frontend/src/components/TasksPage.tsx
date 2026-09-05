// TasksPage — the 任務 page (M3, SPEC §2): 標題 → 篩選列 → 任務清單.
//
//   篩選列  — three dropdowns (執行者 / 類型 / 狀態); any active one surfaces
//             清除篩選. 狀態 is asked of the SERVER (T-a3e4: the fetch carries
//             the ticked set, see setStatuses below) — the page used to
//             download every task and hide most of them. 執行者 / 類型 stay
//             client-side: they are cheap to apply over an already
//             status-narrowed list, and the payload ticket was about status.
//   未結束  — every NON-terminal task in ONE list (狀態不分組 — the status
//             badge differentiates), ordered by priority 高→中→低→凍結 (凍結
//             永遠最後), createdTs newest-first within a level.
//   已結束  — 已完成 + 終止, COLLAPSED BY DEFAULT (the RepliesPage answered-
//             toggle pattern), newest close first. Both section titles carry
//             counts.
//
// Empty states ×2 (spec §2.3): no tasks at all vs filters matching nothing.
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
  // normal layout, cleared by the same 清除篩選 as any other filter.
  // Read BEFORE useTasks so the anchored id reaches the hook in the SAME render
  // the hash lands in: routed through an effect instead, the mount fetch and the
  // page's empty-state decision would both run a commit before the hook knows
  // there is an anchor at all.
  const [route, setRoute] = useHashRoute();
  const taskIdFilter = route.page === "tasks" ? route.taskId : undefined;
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
  } = useTasks(DEFAULT_STATUS, taskIdFilter);

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

  // ── 篩選列 state ───────────────────────────────────────────────────────────
  // Every axis is now MULTI-SELECT (T-be18). A dimension's Set holds the keys
  // the owner ticked; an EMPTY set means "no constraint" (所有人 / 所有類型).
  //   executor keys: "outsource" | "unassigned" | <member id>
  //   type keys:     "adhoc" | <type_key>
  //   status keys:   <one of the six>
  // Status opens at DEFAULT_STATUS (the four non-terminal states) so terminals
  // are excluded by default but visible-and-undoable in the dropdown.
  const [executorFilter, setExecutorFilter] = useState<Set<string>>(
    () => new Set()
  );
  const [typeFilter, setTypeFilter] = useState<Set<string>>(() => new Set());
  const [statusFilter, setStatusFilter] = useState<Set<string>>(
    () => new Set(DEFAULT_STATUS)
  );
  // ── ID 篩選 (T-93, second pass) ────────────────────────────────────────────
  // owner 2026-09-06 on rc-44347fc49338: 「任務沒有出現同樣的filter」. The 任務頁
  // had only HALF of what he asked for — the hash could seed a filter, but there
  // was no field to type one into, while 請示卡頁 had both. His charter said
  // 「任務列表跟請示卡列表，是不是都可以有一個ID的filter」, so this was a gap, not a
  // design choice.
  //
  // 🔴 TWO PATHS MEET HERE AND THEY ARE NOT THE SAME PATH. Keep them apart:
  //  (1) `taskIdFilter` — the HASH anchor. It fetches that ONE task from
  //      `GET /api/tasks/{id}` and OVERRIDES the status set, so a link to a
  //      已完成 task still lands even though the default view hides terminals.
  //  (2) `idQuery` — what the owner TYPES. It filters the tasks already loaded
  //      and asks the server NOTHING. It must not fetch: an independent review
  //      already returned "一個字元一個請求" as a must-fix on 請示卡頁, and a
  //      half-typed id names no task anyway.
  // Consequence, stated rather than hidden: a typed id that belongs to a task
  // outside the current status set matches nothing until 清除篩選 widens the set.
  // That is how the three dropdowns beside it already behave — they narrow what
  // is on screen — so the field is consistent with its neighbours rather than
  // with the anchor.
  const [idFilter, setIdFilter] = useState(taskIdFilter ?? "");
  useEffect(() => {
    if (taskIdFilter) setIdFilter(taskIdFilter);
  }, [taskIdFilter]);
  const idQuery = idFilter.trim().toLowerCase();
  const matchesId = (task: TaskView) =>
    idQuery === "" || task.id.toLowerCase().includes(idQuery);

  // ── 聊天 header 任務圖示 → #tasks/executor/<memberId> (T-dfae). Owner asked
  // for "that member's tasks that aren't done yet", so the seed sets BOTH axes
  // it promises rather than trusting the mount-time defaults: executor = that
  // member, status = the four non-terminal states, type = 所有類型. Doing it
  // explicitly matters — the page is NOT always a fresh mount (a stale
  // statusFilter from an earlier visit would otherwise silently break the
  // "還沒完成" half of the promise, and a live typeFilter would hide rows).
  // One-shot (composeTaskNo precedent): the hash normalises back to #tasks the
  // moment it is consumed, so the seeded filters are ordinary, owner-editable
  // filter state — 清除篩選 and the dropdowns work on them like any other.
  const executorSeed = route.page === "tasks" ? route.executorId : undefined;
  useEffect(() => {
    if (!executorSeed) return;
    setExecutorFilter(new Set([executorSeed]));
    setTypeFilter(new Set());
    setStatusFilter(new Set(DEFAULT_STATUS));
    setRoute({ page: "tasks" });
  }, [executorSeed, setRoute]);
  // A filter is "active" (清除篩選 shows) once any axis narrows the full list:
  // a non-empty executor/type/status set or a single-task anchor. The DEFAULT
  // view counts — its status set hides the terminals, so the button shows from
  // the very first render (T-50bb).
  // 🔴 `idQuery` is its own clause, NOT covered by `taskIdFilter`: the owner can
  // type an id with no hash anchor at all, and he can also empty the FIELD while
  // the hash still carries one. Both states must keep 清除篩選 on screen — the
  // second is the hole 請示卡頁 had (an independent review found it) and 任務頁
  // must not grow it now that it has a field of its own.
  const anyFilter =
    executorFilter.size > 0 ||
    typeFilter.size > 0 ||
    statusFilter.size > 0 ||
    idQuery !== "" ||
    taskIdFilter !== undefined;

  // ── 勾什麼就問什麼 (T-a3e4) ────────────────────────────────────────────────
  // The fetch asks for the statuses the owner has TICKED. ONE view genuinely
  // needs every status and says so by sending nothing: 清除篩選 (an empty set =
  // 所有狀態) — there the owner asked for the whole population, so downloading
  // it is the answer, not a defect.
  //
  // 🔴 A #tasks/<id> jump anchor USED TO be the second such view, and it was the
  // defect (owner 2026-08-01): it may point at a task outside the ticked
  // statuses, so the page dropped the constraint and pulled the whole history —
  // 432 KB / 706 rows to make ONE card appear. It no longer touches this ask at
  // all; `useTasks(…, taskIdFilter)` fetches that single task from
  // `GET /api/tasks/{id}` and merges it in. Sending `undefined` from here again
  // would restore the download, which is what the anchor test pins.
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
  const statusAsk = [...statusFilter].sort().join(",");
  useEffect(() => {
    setStatuses(statusAsk === "" ? [] : statusAsk.split(","));
  }, [statusAsk, setStatuses]);

  function clearFilters() {
    // 清除篩選 = 顯示全部 (T-50bb): every axis to "no constraint" — status
    // EMPTIES too (所有狀態, 已完成/終止 included), no longer back to the
    // default four (the old T-be18 semantics). The single-task anchor is just
    // another filter axis, so it clears with the rest.
    setExecutorFilter(new Set());
    setTypeFilter(new Set());
    setStatusFilter(new Set());
    setIdFilter("");
    if (taskIdFilter) setRoute({ page: "tasks" });
  }

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

  // Per-dimension predicates — an empty set matches everything; otherwise the
  // task's key must be in the set. Split out so the §3.6 jump anchor can
  // short-circuit them entirely (below).
  function matchesExecutor(task: TaskView): boolean {
    return executorFilter.size === 0 || executorFilter.has(executorKeyOf(task));
  }
  function matchesType(task: TaskView): boolean {
    const key = task.typeKey === "" ? "adhoc" : task.typeKey;
    return typeFilter.size === 0 || typeFilter.has(key);
  }
  function matchesStatus(task: TaskView): boolean {
    if (statusFilter.size === 0) return true;
    if (statusFilter.has(task.status)) return true;
    // "reassigning" is an orthogonal LOCK, not a status (T-9ca5) — match it off
    // task.lock (a reassigned task still carries its honest derived status too).
    // 🔴 …but only while the task is OPEN, byte-for-byte the server's
    // taskStatusSetMatch rule (T-a3e4): terminate never clears the lock, so a
    // task terminated mid-handover keeps `lock="reassigning"` forever, and that
    // residue is not an intent. The three copies of this rule (server, here,
    // mock) must stay identical — a divergence means the list the server sent
    // and the list this page shows disagree about the same row.
    if (
      statusFilter.has("reassigning") &&
      task.lock === "reassigning" &&
      !TERMINAL.has(task.status)
    ) {
      return true;
    }
    return false;
  }
  function matches(task: TaskView): boolean {
    // A #tasks/<id> anchor is an explicit "show me THIS task" — it overrides the
    // filter set entirely, so a jump to e.g. a done task still lands even though
    // the default status filter hides terminals (T-4108 regression class).
    if (taskIdFilter) return task.id === taskIdFilter;
    // A TYPED id narrows like any other axis — it does not override the others,
    // because nothing was fetched on its behalf and widening the status set
    // behind the owner's back would contradict the dropdown he can see.
    return (
      matchesId(task) &&
      matchesExecutor(task) &&
      matchesType(task) &&
      matchesStatus(task)
    );
  }

  // ── filter option models (labels + 負責人 counts) ──────────────────────────
  // Per-owner count basis (T-be18 #3): the tasks that WOULD show if this owner
  // were the sole executor pick — i.e. honouring the other live axes (status,
  // type) but not the executor axis itself. So the default count reads as
  // "active tasks on this person", and it moves in step with the status filter
  // (add 已完成 → counts grow). taskIdFilter is ignored here (a single-task
  // anchor isn't a status/type filter).
  const inCountScope = (task: TaskView) =>
    matchesStatus(task) && matchesType(task);
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
    .filter((o) => o.count > 0 || executorFilter.has(o.value));
  const typeFilterOptions: MultiSelectOption[] = [
    ...typeOptions.map((k) => ({ value: k, label: typeNames.get(k) ?? k })),
    { value: "adhoc", label: t.tasks.adhoc },
  ];
  const statusFilterOptions: MultiSelectOption[] = STATUS_OPTIONS.map((s) => ({
    value: s,
    // reassigning is a lock, not a status — its label lives under lockReassigning.
    label: s === "reassigning" ? t.tasks.lockReassigning : t.tasks.status[s],
  }));

  const filtered = tasks.filter(matches);
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
  // 🔴 The way OUT is 清除篩選, which already clears this axis with the rest
  // (see clearFilters) and already shows while it is set (see anyFilter) —
  // that is why the anchor can stay without trapping the owner in the hash.
  //
  // A closed target still auto-expands 已結束 so the one match is visible.
  useEffect(() => {
    if (taskIdFilter) setClosedOpen(true);
  }, [taskIdFilter]);

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
  const nothingAtAll =
    !loading &&
    !error &&
    !anchorPending &&
    !anchorFailed &&
    tasks.length === 0 &&
    taskTotal === 0;
  const nothingMatches =
    !loading &&
    !error &&
    !anchorPending &&
    !anchorFailed &&
    !nothingAtAll &&
    filtered.length === 0;

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
        located={taskIdFilter !== undefined && task.id === taskIdFilter}
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
          {t.tasks.loadError}
        </div>
      )}

      {/* ── 篩選列 (multi-select, T-be18) ── */}
      <div className="tasks__filters">
        {/* 10 characters: owner 2026-09-06 set this by hand — 任務 ids are not a
          * fixed length the way 請示卡 ids are (this station shows `T-93`; the
          * canonical form is `t-` + 12 hex), so there is no measurement to
          * derive it from and he picked one rather than have me invent it. */}
        <IdFilterInput
          value={idFilter}
          onChange={setIdFilter}
          label={t.tasks.filterIdLabel}
          testId="filter-task-id"
          widthCh={10}
        />
        <MultiSelectFilter
          noun={t.tasks.filterExecutorNoun}
          allLabel={t.tasks.filterExecutorAll}
          options={executorOptions}
          selected={executorFilter}
          onChange={setExecutorFilter}
          testId="filter-executor"
        />
        <MultiSelectFilter
          noun={t.tasks.filterTypeNoun}
          allLabel={t.tasks.filterTypeAll}
          options={typeFilterOptions}
          selected={typeFilter}
          onChange={setTypeFilter}
          testId="filter-type"
        />
        <MultiSelectFilter
          noun={t.tasks.filterStatusNoun}
          allLabel={t.tasks.filterStatusAll}
          options={statusFilterOptions}
          selected={statusFilter}
          onChange={setStatusFilter}
          testId="filter-status"
        />
        {anyFilter && (
          <button
            type="button"
            className="tasks__clear-filters"
            data-testid="clear-filters"
            onClick={clearFilters}
          >
            {t.tasks.clearFilters}
          </button>
        )}
      </div>

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
