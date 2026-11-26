// TasksPage — the 任務 page (M3, SPEC §2): 篩選列 → 任務清單.
//
//   篩選列   — a FilterPanel (T-118). Four axes, ALWAYS VISIBLE, each taking
//             effect as soon as it is set. owner 2026-09-06 20:07
//             (c-c3d681fe05da):「不要多filter那一層了,全部拉出來」— so round 3's
//             funnel button, its 取消/套用篩選 pair, its 已篩選 strip and the
//             「案件」 sub-title above the list are all gone.
//             🔴 ONE axis still does not commit on keystroke: 任務編號 waits for
//             Enter or blur, because it changes a SERVER request. That is the
//             surviving half of 「每次都要全部都撈回來才濾不合理」 and the one
//             timing owner named this round. The dropdowns commit at once — a
//             tick is already a whole condition.
//             Every axis is asked of the SERVER (`useTasks`): the page holds the
//             applied sets and renders what comes back.
//   任務清單 — one list, no 未結案 / 已結案 sections: which side shows is the
//             狀態 filter's job, and its 未結案 / 已結案 rows tick either side
//             in one go (owner rc-ceb98bb8884f). The server's page order: open
//             tasks by priority 高→中→低→凍結, newest created first within a
//             level; closed tasks follow, newest close first. The 分組依據
//             selector splits it into collapsible lanes whose keys and counts
//             come from the count. The chosen grouping is remembered in this
//             browser (`oc_tasks_group_by`). Each list reads 50 rows, and 50
//             more when the reader scrolls near the bottom.
//
// Empty states ×2 (spec §2.3): no tasks at all vs filters matching nothing.
// The 30s ticking clock drives every card's 已歷時 / step 耗時 counters (same
// cadence as RepliesPage's 已等你).

import { useEffect, useId, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { taskTypeLabel } from "../lib/taskTypeLabel";
import { DUE_FILTER_KEYS, dueFilterLabel } from "../lib/dueDate";
import { PRIORITIES } from "../lib/taskPriority";
import { readStoredChoice, writeStoredChoice } from "../lib/storedChoice";
import type { TaskGroupBy, TaskView } from "../api/adapter";
import { viewerMayForceTaskDone } from "../api";
import { useTasks } from "../hooks/useTasks";
import { useDueSoonLeadMinutes } from "../hooks/useDueSoonLeadMinutes";
import { useMembers } from "../hooks/useMembers";
import { useHashRoute } from "../lib/hashRoute";
import { TaskCard } from "./TaskCard";
import { IdFilterInput } from "./IdFilterInput";
import {
  MultiSelectFilter,
  type MultiSelectGroup,
  type MultiSelectOption,
} from "./MultiSelectFilter";
import { FilterPanel } from "./FilterPanel";
import { DisplayOptions } from "./DisplayOptions";
import { ChevronRightIcon } from "./icons";
import "./office.css"; // chat composer classes the embedded ReplyComposer reuses
import "./replies.css"; // shared reply-card interior styles (embedded cards)
import "./tasks.css";

const TERMINAL = new Set(["done", "terminated", "duplicated"]);

const STATUS_OPTIONS = [
  "not_started",
  "in_progress",
  "waiting_owner",
  "waiting_external",
  // 可結案 — DERIVED and NOT terminal, so it sits with the other work states and
  // is checked by DEFAULT_STATUS below: a task nobody has closed yet is exactly
  // what the page opens on.
  "ready_for_done",
  // "reassigning" is NO LONGER a status (T-9ca5) — it moved to the orthogonal
  // `task.lock`. It stays a 狀態-filter row for continuity, but its predicate
  // keys off task.lock (on the server) and its label off lockReassigning.
  "reassigning",
  "done",
  "terminated",
  "duplicated",
];

// 一進頁面預設排除 TERMINAL 的每一個狀態（清單見上面那個常數，不在這裡抄第二份；
// 這一行原本寫「兩個終態 (done / terminated)」，比 TERMINAL 少列一個。它在本 repo
// 的第一顆 commit 裡就已經是這樣寫，所以是什麼時候開始對不上的，從這裡查不到）
// — the status filter opens with
// every NON-terminal status checked, so the page shows only live tasks and the
// exclusion is visible (and undoable) right there in the 狀態 dropdown (T-be18).
const DEFAULT_STATUS = STATUS_OPTIONS.filter((s) => !TERMINAL.has(s));

const GROUP_BY_OPTIONS = ["none", "due", "priority", "executor", "type", "status"] as const;
type GroupBy = (typeof GROUP_BY_OPTIONS)[number] & TaskGroupBy;

const GROUP_BY_KEY = "oc_tasks_group_by";

// Every status but duplicated: the server refuses to point a duplicate at one.
const DUPLICATE_TARGET_STATUSES = STATUS_OPTIONS.filter(
  (s) => s !== "duplicated" && s !== "reassigning"
);

const NEAR_BOTTOM_PX = 120;

interface Lane {
  key: string;
  title: string;
  count: number;
}

const sorted = (set: Set<string>) => [...set].sort();

export function TasksPage() {
  const { t } = useI18n();
  const {
    members,
    loading: membersLoading,
    error: membersError,
  } = useMembers();
  // ── 請示 → 任務: a reply card's 查看任務 routes to #tasks/<id>. That id
  // is just another filter dimension — the list narrows to that one task in the
  // normal layout, cleared the same way any other axis is: empty its field.
  // Read BEFORE the axes below so the very first ask already carries it.
  const [route, setRoute] = useHashRoute();
  const taskIdFilter = route.page === "tasks" ? route.taskId : undefined;
  const [appliedId, setAppliedId] = useState(taskIdFilter ?? "");

  // ── THE FOUR AXES (T-118) ────────────────────────────────────────────────
  // ONE copy of each axis: a dropdown click is a complete condition, so it
  // takes effect at once (owner 2026-09-06 c-c3d681fe05da).
  //
  // 🔴 THE ID IS THE ONE EXCEPTION, AND IT IS NOT AN OVERSIGHT. It keeps a
  // second piece of state (`draftId`, what is in the box) because a half-typed
  // id is not a condition — and because an applied id is a SERVER request, so
  // committing per keystroke is a fetch per keystroke. owner rejected that
  // shape twice (「每次都要全部都撈回來才濾不合理」) and named the replacement:
  // 「按enter或是點外面就視為apply」. `draftId` → `appliedId` happens on Enter
  // and on blur, nowhere else.
  //
  // A dimension's Set holds the keys ticked; an EMPTY set means "no constraint".
  //   executor keys: "outsource" | "unassigned" | <member id>
  //   type keys:     "adhoc" | <type_key>
  //   status keys:   <one of STATUS_OPTIONS>
  //   due keys:      "overdue" | "soon", OR'd together
  //
  // 狀態 opens at DEFAULT_STATUS — EXCEPT when the page opens on a #tasks/<id>
  // link: a jump means 「給我看這一張」, so it lands with no other axis narrowing
  // anything, seeded in the initialiser so the very first ask agrees.
  const [appliedExecutor, setAppliedExecutor] = useState<Set<string>>(
    () => new Set()
  );
  const [appliedType, setAppliedType] = useState<Set<string>>(() => new Set());
  const [appliedDue, setAppliedDue] = useState<Set<string>>(() => new Set());
  const [appliedStatus, setAppliedStatus] = useState<Set<string>>(() =>
    taskIdFilter ? new Set<string>() : new Set(DEFAULT_STATUS)
  );
  const [draftId, setDraftId] = useState(taskIdFilter ?? "");

  /** Enter or blur on the 任務編號 field — the only two events that turn typed
   * text into a filter (owner 2026-09-06:「按enter或是點外面就視為apply了」). */
  function commitId(next: string) {
    const nextId = next.trim();
    // Committing the SAME value must not re-ask: this fires on blur too.
    if (nextId === appliedId) {
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
  // The applied id goes to the server as `?ids=`, ANDed with every other axis:
  // an EXACT match on one task, not a substring of the loaded rows. A link
  // carries no opinion about 負責人/類型/狀態/到期, so it clears them — that is
  // what keeps a link to a 已完成 task landing: there is no status axis to fail.
  useEffect(() => {
    if (!taskIdFilter) return;
    setAppliedId(taskIdFilter);
    setDraftId(taskIdFilter);
    setAppliedExecutor(new Set());
    setAppliedType(new Set());
    setAppliedDue(new Set());
    setAppliedStatus(new Set());
  }, [taskIdFilter]);

  // ── 聊天 header 任務圖示 → #tasks/executor/<memberId> (T-dfae). Owner asked
  // for "that member's tasks that aren't done yet", so the seed sets BOTH axes
  // it promises rather than trusting the mount-time defaults (the page is not
  // always a fresh mount), and clears any applied id: this jump is about a
  // person, not about one task. One-shot: the hash normalises back to #tasks.
  const executorSeed = route.page === "tasks" ? route.executorId : undefined;
  useEffect(() => {
    if (!executorSeed) return;
    setAppliedExecutor(new Set([executorSeed]));
    setAppliedType(new Set());
    setAppliedDue(new Set());
    setAppliedStatus(new Set(DEFAULT_STATUS));
    setAppliedId("");
    // 🔴 THE BOX TOO: the field commits on blur, so a stale draft would
    // silently re-apply the id the moment the owner clicks into and out of it.
    setDraftId("");
    setRoute({ page: "tasks" });
  }, [executorSeed, setRoute]);

  // 清除篩選 (owner c-2423dba8b65b:「清除篩選還是要留著」) = 顯示全部 (T-50bb):
  // every axis to "no constraint" — status EMPTIES too, NOT back to the default.
  function clearFilters() {
    setAppliedExecutor(new Set());
    setAppliedType(new Set());
    setAppliedDue(new Set());
    setAppliedStatus(new Set());
    setAppliedId("");
    setDraftId("");
    if (taskIdFilter) setRoute({ page: "tasks" });
  }
  // 🔴 The DEFAULT view counts as filtered: its status set hides the
  // terminals, so 清除篩選 is on from the first render.
  const anyFilter =
    appliedExecutor.size > 0 ||
    appliedType.size > 0 ||
    appliedDue.size > 0 ||
    appliedStatus.size > 0 ||
    appliedId !== "";

  // The grouping is remembered per browser; the lane collapse state, like the
  // filters, lives only in this mount. The default-open lane is picked when
  // the default is applied and then held by key: a lane that appears ahead of
  // it later (a task ticks overdue, an SSE arrival) must not collapse the one
  // being read. Only when that lane empties out does the default move to
  // whatever lane is first.
  const [groupBy, setGroupByState] = useState<GroupBy>(() =>
    readStoredChoice(GROUP_BY_KEY, GROUP_BY_OPTIONS, "none")
  );
  const [laneOpenOverrides, setLaneOpenOverrides] = useState<
    Map<string, boolean>
  >(() => new Map());
  const [defaultOpenKey, setDefaultOpenKey] = useState<string | null>(null);
  function setGroupBy(next: GroupBy) {
    setGroupByState(next);
    writeStoredChoice(GROUP_BY_KEY, next);
    setLaneOpenOverrides(new Map());
    setDefaultOpenKey(null);
  }
  function toggleLane(key: string, expandedNow: boolean) {
    setLaneOpenOverrides((cur) => new Map(cur).set(key, !expandedNow));
  }

  const query = {
    statuses: sorted(appliedStatus),
    executors: sorted(appliedExecutor),
    types: sorted(appliedType),
    due: sorted(appliedDue),
    ids: appliedId === "" ? [] : [appliedId],
    groupBy,
  };

  // The lanes come from the count, so the lists to read are decided from the
  // count the hook last answered — passed back in on the next render.
  const [listKeys, setListKeys] = useState<string[]>(groupBy === "none" ? [""] : []);
  const {
    lists,
    count,
    answeredQuery,
    settled,
    loadMore,
    loadingMore,
    workers,
    taskTypes,
    loading,
    error,
    searchTasks,
    terminate,
    forceDone,
    markDuplicate,
    setPriority,
    setDue,
    reassign,
    sendMessage,
    getDetail,
    removeArtifact,
  } = useTasks(query, listKeys);

  // Cards color their own 到期日 off the workspace lead; the lists' buckets are
  // the server's.
  const dueSoonLeadMinutes = useDueSoonLeadMinutes();

  // Ticking clock (30s) — drives 已歷時 and running-step 耗時 on every card.
  const [nowTs, setNowTs] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const timer = window.setInterval(
      () => setNowTs(Date.now() / 1000),
      30_000
    );
    return () => window.clearInterval(timer);
  }, []);

  // Executor options: 外包 / 未指派 / 各成員 (real AI members only — machine-
  // layer wardens are not executors). An empty set = 所有人.
  const memberOptions = members.filter((m) => m.kind === "staff");
  const heldRows = [...lists.values()].flatMap((l) => l.tasks);
  // Type options: 各手冊類型 (the manuals list) ∪ any type a held row or a type
  // lane names (covers a type whose manual was since deleted — closed tasks
  // keep it) ∪ what is ticked, so a ticked type can always be unticked.
  const typeOptions = [
    ...new Set([
      ...taskTypes.map((x) => x.typeKey),
      ...heldRows.map((x) => x.typeKey).filter((k) => k !== ""),
      ...(groupBy === "type" ? count?.lanes ?? [] : []).map((l) => l.key),
      ...appliedType,
    ]),
  ]
    .filter((k) => k !== "adhoc")
    .sort();
  // type_key → display name (T-fa76): the filter labels and the cards' type
  // chips show the manual's human label; a deleted manual's key honestly
  // falls back to itself.
  const typeNames = new Map(
    taskTypes
      .filter((x) => x.displayName !== "")
      .map((x) => [x.typeKey, taskTypeLabel(t, x.typeKey, x.displayName)])
  );

  // ── filter option models (labels + 負責人 counts) ──────────────────────────
  // Per-owner count basis (T-be18 #3): the tasks that WOULD show if this owner
  // were the sole executor pick — the other axes honoured, the executor axis
  // and the id not. The server counts them (`executorCounts`).
  const executorCounts = count?.executorCounts ?? {};
  const executorOptions: MultiSelectOption[] = [
    { value: "outsource", label: t.tasks.outsource },
    { value: "unassigned", label: t.tasks.unassigned },
    ...memberOptions.map((m) => ({ value: m.id, label: m.name })),
  ]
    .map((o) => ({ ...o, count: executorCounts[o.value] ?? 0 }))
    // 負責人下拉只列在當前 status/type 結果集中有任務的執行者 (owner 回饋:計數
    // 0 者隱藏;外包與未指派同規則,T-be18 #3)。邊界:已勾選的執行者即使計數
    // 歸 0 也保留 — 否則使用者無法取消勾選、勾選態會卡死。
    .filter((o) => o.count > 0 || appliedExecutor.has(o.value));
  const typeFilterOptions: MultiSelectOption[] = [
    ...typeOptions.map((k) => ({ value: k, label: typeNames.get(k) ?? k })),
    { value: "adhoc", label: t.tasks.adhoc },
  ];
  const dueFilterOptions: MultiSelectOption[] = DUE_FILTER_KEYS.map((k) => ({
    value: k,
    label: dueFilterLabel(t.tasks, k),
  }));
  const statusGroups: MultiSelectGroup[] = [
    { value: "open", label: t.tasks.openTitle, members: DEFAULT_STATUS },
    { value: "closed", label: t.tasks.closedTitle, members: [...TERMINAL] },
  ];
  const statusFilterOptions: MultiSelectOption[] = STATUS_OPTIONS.map((s) => ({
    value: s,
    // reassigning is a lock, not a status — its label lives under lockReassigning.
    label: s === "reassigning" ? t.tasks.lockReassigning : t.tasks.status[s],
  }));

  const laneIdBase = useId();
  const groupByLabels: Record<GroupBy, string> = {
    due: t.tasks.groupByDue,
    executor: t.tasks.groupByExecutor,
    type: t.tasks.groupByType,
    status: t.tasks.groupByStatus,
    priority: t.tasks.groupByPriority,
    none: t.tasks.groupByNone,
  };

  function laneTitle(key: string): string {
    switch (groupBy) {
      case "due":
        return (
          {
            overdue: t.tasks.laneOverdue,
            soon: t.tasks.laneDueSoon,
            later: t.tasks.laneDueLater,
            nodue: t.tasks.laneNoDue,
          }[key] ?? key
        );
      case "priority":
        return (PRIORITIES as readonly string[]).includes(key)
          ? t.tasks.priority[key as (typeof PRIORITIES)[number]]
          : key;
      case "executor":
        if (key === "outsource") return t.tasks.outsource;
        if (key === "unassigned") return t.tasks.unassigned;
        return memberOptions.find((m) => m.id === key)?.name ?? key;
      case "type":
        return key === "adhoc" ? t.tasks.adhoc : typeNames.get(key) ?? key;
      case "status":
        return t.tasks.status[key] ?? key;
      case "none":
        return key;
    }
  }

  // The server orders the lanes (spec: TaskCountDTO.lanes); the page only
  // titles them.
  const lanes: Lane[] =
    groupBy === "none" || count === null || answeredQuery?.groupBy !== groupBy
      ? []
      : count.lanes.map((l) => ({ key: l.key, count: l.count, title: laneTitle(l.key) }));
  if (
    lanes.length > 0 &&
    !lanes.some((lane) => lane.key === defaultOpenKey)
  ) {
    setDefaultOpenKey(lanes[0].key);
  }
  const openByDefault =
    lanes.find((lane) => lane.key === defaultOpenKey)?.key ?? lanes[0]?.key;
  const laneExpanded = (lane: Lane) =>
    laneOpenOverrides.get(lane.key) ?? lane.key === openByDefault;
  const shownKeys =
    groupBy === "none" ? [""] : lanes.filter(laneExpanded).map((l) => l.key);
  if (JSON.stringify(shownKeys) !== JSON.stringify(listKeys)) setListKeys(shownKeys);
  const laneLead: string | undefined = {
    due: undefined,
    executor: t.tasks.laneExecutorLead,
    type: t.tasks.laneTypeLead,
    status: t.tasks.laneStatusLead,
    priority: t.tasks.lanePriorityLead,
    none: undefined,
  }[groupBy];

  const flat =
    groupBy === "none" && answeredQuery?.groupBy === "none" ? lists.get("")?.tasks ?? [] : [];

  // Scrolling near the bottom reads the next page of every list on screen
  // that has more.
  const moreRef = useRef<() => void>(() => {});
  moreRef.current = () => {
    for (const key of listKeys) {
      const list = lists.get(key);
      if (list && !list.complete && !loadingMore.has(key)) loadMore(key);
    }
  };
  const scrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    function onScroll() {
      if (!el) return;
      if (el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX) {
        moreRef.current();
      }
    }
    el.addEventListener("scroll", onScroll);
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  // An applied id expands the lane its task is in: with the id on there is
  // only that lane, and it must be open, or the page would say "found" while
  // showing no row at all. Keyed on the APPLIED id, not the hash — a typed id
  // names a task exactly as often as a link does.
  useEffect(() => {
    if (appliedId === "") return;
    setLaneOpenOverrides(new Map());
    setDefaultOpenKey(null);
  }, [appliedId]);

  // ── empty states ─────────────────────────────────────────────────────────
  // Neither speaks until the lists answer the CURRENT ask: until then what is
  // held answers the previous one. A failed read says so instead.
  //
  // 目前沒有任務 is a claim about the WHOLE workshop, so it rests on the
  // count's unfiltered `total`, never on an empty list.
  //
  // 🔴 AN APPLIED ID USES THE ORDINARY EMPTY STATE — owner 2026-09-06
  // (rc-f603bbd447f4 「為什麼要顯示這種東西 拿掉!」, c-86c129855835 「不用 比數本來
  // 就是要顯示篩選過的數量」). An id that names nothing and an id excluded by
  // another axis both answer 沒有符合篩選條件的任務; do not "restore" by-id
  // notices as a bug fix.
  const idApplied = appliedId !== "";
  const answered = settled && !error;
  const listedNothing =
    groupBy === "none" ? flat.length === 0 && lists.has("") : lanes.length === 0;
  const nothingAtAll =
    !idApplied && answered && listedNothing && (count?.total ?? 0) === 0;
  const nothingMatches = answered && !nothingAtAll && listedNothing;

  function renderCard(task: TaskView) {
    return (
      <TaskCard
        key={task.id}
        task={task}
        members={members}
        workers={workers}
        rosterSettled={!membersLoading && !membersError && !loading && !error}
        typeNames={typeNames}
        nowTs={nowTs}
        located={idApplied && task.id === appliedId}
        onTerminate={terminate}
        onForceDone={forceDone}
        // Read HERE, once per render, rather than inside the card: the card
        // stays a pure function of its props. What it means and what it does
        // NOT defend is documented on `viewerMayForceTaskDone` itself — the
        // server's route floor is the gate.
        canForceDone={viewerMayForceTaskDone()}
        onMarkDuplicate={markDuplicate}
        onSearchDuplicateTargets={(q) => searchTasks(q, DUPLICATE_TARGET_STATUSES)}
        onSetPriority={setPriority}
        onSetDue={setDue}
        dueSoonLeadMinutes={dueSoonLeadMinutes}
        onReassign={reassign}
        onSendMessage={(id, body, attachments) =>
          sendMessage(id, { body, attachments })
        }
        onHydrate={getDetail}
        onRemoveArtifact={removeArtifact}
      />
    );
  }

  function renderMore(key: string) {
    return loadingMore.has(key) ? (
      <div className="tasks__more" data-testid={`tasks-loading-more${key === "" ? "" : `-${key}`}`}>
        {t.tasks.loadingMore}
      </div>
    ) : null;
  }

  function renderLane(lane: Lane) {
    const expanded = laneExpanded(lane);
    const regionId = `${laneIdBase}-lane-${lane.key}`;
    return (
      <section
        key={lane.key}
        className="tasks__section"
        data-testid={`lane-${lane.key}`}
      >
        <button
          type="button"
          id={`${regionId}-toggle`}
          className="tasks__section-toggle tasks__lane-toggle"
          aria-expanded={expanded}
          aria-controls={regionId}
          data-testid={`lane-toggle-${lane.key}`}
          onClick={() => toggleLane(lane.key, expanded)}
        >
          <ChevronRightIcon
            size={13}
            className={`reply-card__caret${
              expanded ? " reply-card__caret--open" : ""
            }`}
          />
          <span>
            {laneLead}
            {lane.title}
          </span>
          <span data-testid={`lane-count-${lane.key}`}>
            {` · ${lane.count}`}
          </span>
        </button>
        <div
          id={regionId}
          role="region"
          aria-labelledby={`${regionId}-toggle`}
          className="tasks__list"
          hidden={!expanded}
          aria-busy={expanded && !lists.has(lane.key)}
        >
          {expanded && (lists.get(lane.key)?.tasks ?? []).map(renderCard)}
          {expanded && renderMore(lane.key)}
        </div>
      </section>
    );
  }

  return (
    <div
      className="tasks"
      ref={scrollRef}
      aria-busy={!settled || listKeys.some((k) => !lists.has(k))}
    >
      {error && (
        <div className="tasks__error" data-testid="tasks-error">
          {/* A failed by-id read is NOT 找不到 — it is 沒問到. Same box, and
            * deliberately different words: the owner must never read a broken
            * server as a deleted task. */}
          {idApplied ? t.tasks.idUnreached(appliedId) : t.tasks.loadError}
        </div>
      )}

      {/* ── 篩選列 (T-118) — 四個軸,常駐可見,選了就生效 ── */}
      <FilterPanel
        testId="tasks-filter"
        clearLabel={t.tasks.clearFilters}
        onClear={anyFilter ? clearFilters : undefined}
        actions={
          <DisplayOptions
            label={t.tasks.groupByLabel}
            value={groupBy}
            options={GROUP_BY_OPTIONS.map((g) => ({ value: g, label: groupByLabels[g] }))}
            onChange={setGroupBy}
            selectTestId="group-by"
          />
        }
      >
        {/* 🔴 A second comment used to sit here saying the count was 10, set by
          * hand, and that the canonical 任務 id was `t-` + 12 hex. BOTH halves
          * were false by the time anyone read them: owner replaced the 10 on
          * 2026-09-07 (`rc-b2beb7b1fd3c` 「任務可先假設到萬位數」 — see the note
          * on `widthCh` below), and `t-` + 12 hex is the OLD, unmigrated shape
          * — canonical is `T-` + a sequence number (dal_task_id_seq.go, and
          * docs/design/SPEC.md agrees). It is DELETED rather than corrected
          * because the note on `widthCh` already carries the whole record:
          * two comments on one element is what let them drift apart, and a
          * stale one here points the next reader at the guard instead of the
          * bug — 7 would look like someone's slip and the red assertion like
          * the thing to 「fix」. */}
        <IdFilterInput
          value={draftId}
          onChange={setDraftId}
          onCommit={commitId}
          label={t.tasks.filterIdLabel}
          testId="filter-task-id"
          // 「T-」 + up to five digits. owner 2026-09-07 `rc-b2beb7b1fd3c`:
          // 「ID寬度要合理…任務可先假設到萬位數」 — it replaces the 10 he had set
          // by hand the day before. ⚠️ This is the ID's share only; the field
          // also has to hold its own label and takes whichever is wider (see
          // IdFilterInput), so on this page the visible width is the label's.
          widthCh={7}
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
          groups={statusGroups}
          selected={appliedStatus}
          onChange={setAppliedStatus}
          testId="filter-status"
        />
        <MultiSelectFilter
          noun={t.tasks.filterDueNoun}
          allLabel={t.tasks.filterDueAll}
          options={dueFilterOptions}
          selected={appliedDue}
          onChange={setAppliedDue}
          testId="filter-due"
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

      {groupBy !== "none" && lanes.length > 0 && (
        <div className="tasks__lanes" data-testid="task-list">
          {lanes.map(renderLane)}
        </div>
      )}
      {groupBy === "none" && flat.length > 0 && (
        <div className="tasks__list" data-testid="task-list">
          {flat.map(renderCard)}
          {renderMore("")}
        </div>
      )}
    </div>
  );
}
