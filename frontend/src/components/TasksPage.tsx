// TasksPage — the 任務 page (M3, SPEC §2): 標題 → 篩選列 → 任務清單.
//
//   篩選列  — three dropdowns (執行者 / 類型 / 狀態); any active one surfaces
//             清除篩選. Filtering is CLIENT-SIDE over the one unfiltered list
//             (see api.listTasks) — instant, and one SSE refetch path.
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
import { useMembers } from "../hooks/useMembers";
import { useHashRoute } from "../lib/hashRoute";
import { TaskCard } from "./TaskCard";
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
    setIncludeClosed,
  } = useTasks();

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
  // ── 請示 → 任務: a reply card 查看任務詳情 routes to #tasks/<id>. That id
  // is just another filter dimension — the list narrows to that one task in the
  // normal layout, cleared by the same 清除篩選 as any other filter.
  const [route, setRoute] = useHashRoute();
  const taskIdFilter = route.page === "tasks" ? route.taskId : undefined;
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
  const anyFilter =
    executorFilter.size > 0 ||
    typeFilter.size > 0 ||
    statusFilter.size > 0 ||
    taskIdFilter !== undefined;

  // T-2b9d: the list loads 未結束-only by default (open=true) — the fast path
  // the default view actually renders. The moment the current filters COULD
  // surface a terminal (已結束) task, ask useTasks to (re)load the full
  // population: an empty status set (清除篩選 = 全部), any terminal status
  // ticked, or a single-task jump anchor (which bypasses the status filter and
  // may target a closed task). Back in the default view it flips false and the
  // hot path re-optimises. matches()'s status gate makes the 已結束 partition
  // empty whenever the filter excludes terminals, so this is exactly the set of
  // views that need closed data — no more, no less.
  const needClosed =
    taskIdFilter !== undefined ||
    statusFilter.size === 0 ||
    [...statusFilter].some((s) => TERMINAL.has(s));
  useEffect(() => {
    setIncludeClosed(needClosed);
  }, [needClosed, setIncludeClosed]);

  function clearFilters() {
    // 清除篩選 = 顯示全部 (T-50bb): every axis to "no constraint" — status
    // EMPTIES too (所有狀態, 已完成/終止 included), no longer back to the
    // default four (the old T-be18 semantics). The single-task anchor is just
    // another filter axis, so it clears with the rest.
    setExecutorFilter(new Set());
    setTypeFilter(new Set());
    setStatusFilter(new Set());
    if (taskIdFilter) setRoute({ page: "tasks" });
  }

  // Executor options: 外包 / 未指派 / 各成員 (real AI members only — machine-
  // layer wardens are not executors). An empty set = 所有人.
  const memberOptions = members.filter((m) => m.kind !== "warden");
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
    if (statusFilter.has("reassigning") && task.lock === "reassigning") {
      return true;
    }
    return false;
  }
  function matches(task: TaskView): boolean {
    // A #tasks/<id> anchor is an explicit "show me THIS task" — it overrides the
    // filter set entirely, so a jump to e.g. a done task still lands even though
    // the default status filter hides terminals (T-4108 regression class).
    if (taskIdFilter) return task.id === taskIdFilter;
    return matchesExecutor(task) && matchesType(task) && matchesStatus(task);
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

  // A #tasks/<id> filter on an unknown/stale task self-heals to the full list;
  // a closed target auto-expands 已結束 so the one match is actually visible.
  useEffect(() => {
    if (
      taskIdFilter &&
      !loading &&
      !tasks.some((x) => x.id === taskIdFilter) &&
      (tasks.length > 0 || !error)
    ) {
      setRoute({ page: "tasks" });
    }
  }, [taskIdFilter, loading, error, tasks, setRoute]);
  useEffect(() => {
    if (taskIdFilter) setClosedOpen(true);
  }, [taskIdFilter]);

  const nothingAtAll = !loading && !error && tasks.length === 0;
  const nothingMatches =
    !loading && !error && tasks.length > 0 && filtered.length === 0;

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
      {error && <div className="tasks__error">{t.tasks.loadError}</div>}

      {/* ── 篩選列 (multi-select, T-be18) ── */}
      <div className="tasks__filters">
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
