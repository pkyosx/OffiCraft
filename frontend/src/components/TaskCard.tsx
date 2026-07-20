// TaskCard — one M3 任務卡 (spec §3: all information on the card, no detail
// page). COLLAPSED BY DEFAULT (mockup, owner 2026-07-13): the card head +
// keys + progress + waiting banner + message box always show; CLICKING THE
// CARD (anywhere that is not an interactive element — owner mobile refactor
// 2026-07-17, the chevron button is gone) expands the workflow timeline +
// the embedded reply cards + the description. Top to bottom:
//
//   head      — row 1 is the fixed badge row (☑ #T-xxxx id badge · 優先權 chip
//               (click → select-style dropdown, 高/中/低/凍結 incl. freeze) ·
//               狀態 badge (click → owner-action dropdown, ALWAYS — 標記重複 /
//               終止, greyed + unclickable once the task is closed, with the
//               等我回覆 jump as an extra item while waiting)) with
//               the ▸/▾ expand indicator owning the top-right corner (the ⋮
//               owner menu was deleted once its items moved to that dropdown —
//               owner 2026-07-17); row 2 is the title; then the label column: 任務類型 /
//               負責人 / 建立者 / 識別鍵 (URL values open target=_blank). The 負責人
//               value carries a 轉派 icon button to its right (SwapIcon — a
//               two-way swap glyph; click → TaskReassignDialog),
//               owner 2026-07-18: 轉派 is a hand-over-to-a-new-executor action,
//               so it lives next to WHO owns the task, not in the 狀態 dropdown
//               (which owner emptied of everything but 標記重複/終止) nor in a
//               restored ⋮ menu. Shown only while the task is non-terminal —
//               the server 409s a reassign on a closed task.

//   progress  — the SERVER-computed 完成 N／總 N (never recomputed here) + a
//               bar, and 已歷時 ticking from created_ts (closed tasks freeze
//               at closed_ts — honest, the clock stops when the task does).
//   waiting   — the one-line waiting_reason row while 等待外部.
//   deps      — 被 T-xxxx 擋住 chips (task ids resolved to display task_no).
//   message   — 傳訊息給 {executor}… box (POST /api/tasks/{id}/message;
//               disabled while unassigned — the server would 409). Text
//               and/or attachments: paste an image / pick files via the
//               paperclip (the shared useAttachmentStaging machine, same as
//               the chat composer / ReplyComposer).
//   desc      — the 詳細敘述 markdown, BELOW the message box (owner 2026-07-17
//               「task details 應該在回覆訊息的下面」 — only this block moved;
//               the head's meta column stayed put). Expanded-only.
//   workflow  — the step timeline: name + DoD + status badge + 耗時; parallel
//               stages grouped 「同時進行 · N 項並行」; gate projection dashed
//               (announced) vs solid (armed).
//               Card-bearing steps embed their reply card INSIDE the step row
//               (owner 2026-07-14: 卡內嵌在所屬 step 內, 不再懸在 task 頂部;
//               TaskReplyCard — the SHARED M2 ReplyCardBody interior, never a
//               re-implementation; answered cards collapse to one line).
//               Steps that EVER carried an approval (is_gate, or a bound
//               reply card — auto-bound plain asks included) keep a permanent
//               審批 marker after they finish (owner 2026-07-14: 持久標記).
//               Transitional states when there are no steps yet: unassigned →
//               等待指派; assigned-but-no-plan → 「等待 ○○ 建立 Steps」 (○○ = the
//               executor's display name; owner 2026-07). But a task the light
//               list already counted leaves for (progressTotal > 0) whose detail
//               has none yet shows the LOADING placeholder, never that copy —
//               progress says a plan exists, so the empty state would lie
//               (T-71e8, aligned with the detailPending/detailFailed guards).

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useWorkerCodenames } from "../hooks/useWorkerCodenames";
import { useI18n } from "../i18n";
import type { Member } from "../types";
import type {
  ChatAttachmentInput,
  TaskView,
  TaskStepView,
  TaskReassignInput,
  OutsourceWorkerView,
} from "../api/adapter";
import { formatDuration } from "../lib/duration";
import { copyText } from "../lib/clipboard";
import { resolveStepBadge } from "../lib/stepBadge";
import { autosizeTextarea } from "../lib/autosize";
import { navigateHash } from "../lib/hashRoute";
import {
  ATTACH_ACCEPT,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { ConfirmModal } from "./ConfirmModal";
import { Markdown } from "./Markdown";
import { TaskArtifactsBadge } from "./TaskArtifactsPopover";
import { TaskReassignDialog } from "./TaskReassignDialog";
import { TaskReplyCard } from "./TaskReplyCard";
import {
  ChatBubbleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  ExternalLinkIcon,
  GearIcon,
  PaperclipIcon,
  SendIcon,
  SwapIcon,
  TasksIcon,
} from "./icons";

/** Terminal statuses (已完成/終止/重複) — owner actions and ticking stop here. */
const TERMINAL = new Set(["done", "terminated", "duplicated"]);

const PRIORITIES = ["high", "mid", "low", "frozen"] as const;

/** True when the 識別鍵 value is a link → the badge renders target=_blank. */
function isUrl(v: string): boolean {
  return /^https?:\/\//i.test(v);
}

/** One rendered timeline row: a single leaf, or one parallel stage of leaves
 * (consecutive steps sharing a non-empty parallelGroup). */
type TimelineRow =
  | { kind: "single"; step: TaskStepView }
  | { kind: "parallel"; group: string; steps: TaskStepView[] };

function buildTimeline(steps: TaskStepView[]): TimelineRow[] {
  const rows: TimelineRow[] = [];
  for (const step of steps) {
    const last = rows[rows.length - 1];
    if (
      step.parallelGroup &&
      last?.kind === "parallel" &&
      last.group === step.parallelGroup
    ) {
      last.steps.push(step);
    } else if (step.parallelGroup) {
      rows.push({ kind: "parallel", group: step.parallelGroup, steps: [step] });
    } else {
      rows.push({ kind: "single", step });
    }
  }
  return rows;
}

export function TaskCard({
  task,
  allTasks,
  members,
  workers,
  typeNames,
  nowTs,
  located = false,
  onTerminate,
  onMarkDuplicate,
  onSetPriority,
  onReassign,
  onSendMessage,
  onHydrate,
  onRemoveArtifact,
}: {
  task: TaskView;
  /** The whole list — dep ids resolve to display task_no through it. */
  allTasks: TaskView[];
  members: Member[];
  /** LIVE outsource workers — resolves the 外包 codename/model/effort. */
  workers: OutsourceWorkerView[];
  /** type_key → manual display name (T-fa76): the 類型 chip shows the human
   * label; an absent entry (deleted manual / old task) falls back to the raw
   * key. OPTIONAL so hand-built fixtures stay valid. */
  typeNames?: Map<string, string>;
  /** The page's shared ticking clock (30s) — drives 已歷時 and step 耗時. */
  nowTs: number;
  /** §3.6 跳轉定位: true while this card is the just-located jump target —
   * renders the transient highlight pulse (same language as the chat's
   * 跳到原訊息 flash). */
  located?: boolean;
  onTerminate: (id: string) => Promise<void>;
  /** Mark this task a duplicate of `duplicateOf` (T-02c9). The server enforces
   * the depth-1 graph + non-terminal guard; a rejection surfaces inline. */
  onMarkDuplicate: (id: string, duplicateOf: string) => Promise<void>;
  onSetPriority: (id: string, priority: string) => Promise<void>;
  /** 轉派 (T-160e): hand the task to a member / a freshly minted 外包. The whole
   * handover is the server's; the dialog only names the target. */
  onReassign: (id: string, input: TaskReassignInput) => Promise<void>;
  onSendMessage: (
    id: string,
    body: string,
    attachments: ChatAttachmentInput[]
  ) => Promise<void>;
  /** Fetch this task's FULL detail (steps + description) — the list serves only
   * the light projection, so the card hydrates the workflow timeline on expand
   * (and re-hydrates when the task's updatedTs moves while it stays open). */
  onHydrate: (id: string) => Promise<TaskView>;
  /** Owner/admin un-pin of one artifact (T-3dc5). Absent ⇒ the artifact popover
   * is display-only (no × affordance). */
  onRemoveArtifact?: (taskId: string, artifactId: string) => Promise<void>;
}) {
  const { t } = useI18n();
  const closed = TERMINAL.has(task.status);
  const unassigned = task.executorKind === "outsource" && !task.executorId;

  // ── executor identity ──────────────────────────────────────────────────────
  // RELEASED-worker codenames (T-3ed8): an ow- id on the card (executor of a
  // closed task, 前任, creator) drops off the LIVE workers list on release —
  // resolve its codename lazily via the per-id read so the chip shows the same
  // identity the rail did, never the raw id.
  const releasedCodenames = useWorkerCodenames(
    [task.executorId, task.creatorId, task.reassignedFrom ?? ""].filter(
      (id) => id.startsWith("ow-") && !workers.some((w) => w.id === id),
    ),
  );
  const member =
    task.executorKind === "member"
      ? members.find((m) => m.id === task.executorId)
      : undefined;
  const worker =
    task.executorKind === "outsource" && task.executorId
      ? workers.find(
          (w) => w.id === task.executorId || w.taskId === task.id
        )
      : undefined;
  // 外包 display: 「外包 代號 · 模型 · 投入度」 while the worker is LIVE; a
  // released worker (closed task) keeps its REAL codename via the lazy per-id
  // read (never fabricated — resolved from the server row), degrading to the
  // bare 外包 label only while/if unresolvable. A member is just the bare name
  // (T-705e: the chip sits under the 負責人 label, so the old "· 成員" role tag
  // is redundant and the spec chip shows the name alone — 外包 keeps its
  // substantive 代號·模型·投入度 line).
  const releasedExecutorCodename = releasedCodenames.get(task.executorId);
  const executorText = unassigned
    ? t.tasks.unassigned
    : task.executorKind === "member"
      ? member?.name || task.executorId
      : worker
        ? `${t.tasks.outsource} ${worker.codename} · ${worker.model || "—"} · ${
            t.tasks.effortOf[worker.effort] ?? worker.effort
          }`
        : releasedExecutorCodename
          ? `${t.tasks.outsource} ${releasedExecutorCodename}`
          : t.tasks.outsource;
  // 類型 chip text: the manual's display name (T-fa76), falling back to the
  // raw key (deleted manual / legacy type), then the ad-hoc word.
  const typeLabel = task.typeKey
    ? typeNames?.get(task.typeKey) || task.typeKey
    : t.tasks.adhoc;

  // ── header rows: type / assignee / creator jump wiring (T-e987) ────────────
  // Type row → the task-type settings hub (#settings/manuals/<typeKey>); an
  // ad-hoc task (typeKey "") has no settings page, so the label is plain text.
  const typeClickable = task.typeKey !== "";
  function openTypeSettings() {
    navigateHash({ page: "settings", manualKey: task.typeKey });
  }
  // A chat jump seeds the peer's composer with "[<taskNo>] " (compose segment).
  function openChat(peerId: string) {
    navigateHash({ page: "office", chatId: peerId, composeTaskNo: task.taskNo });
  }
  // Assignee row → the executor's chat. Any resolvable executor (a member, a
  // live OR released outsource worker — history is keyed by peer id, T-661b) is
  // reachable; only 未指派 (outsource with no executorId) is not clickable.
  const assigneePeerId = unassigned ? "" : task.executorId;

  // Creator row → the creator's chat. The creator is a verified token sub: a
  // roster member (name, clickable), an outsource worker id ("ow-…" → 外包
  // 代號 when live, else the raw id; chat reachable either way), "" on a
  // pre-column task ("—", not clickable), or a non-member/non-worker value
  // ("owner" or anything else) which is shown as plain text and NOT clickable —
  // there is no self-chat, and fabricating one would be a lie (owner ruling).
  const creator = ((): { text: string; peerId: string } => {
    if (task.creatorId === "") return { text: t.tasks.creatorUnknown, peerId: "" };
    const m = members.find((x) => x.id === task.creatorId);
    if (m) return { text: m.name, peerId: task.creatorId };
    if (task.creatorId.startsWith("ow-")) {
      const cn =
        workers.find((x) => x.id === task.creatorId)?.codename ??
        releasedCodenames.get(task.creatorId);
      return {
        text: cn ? `${t.tasks.outsource} ${cn}` : task.creatorId,
        peerId: task.creatorId,
      };
    }
    return { text: task.creatorId, peerId: "" };
  })();

  // 前任 row (T-ba04 轉派交接) → the predecessor's chat. Same resolution shape
  // as the creator row: a member (name, clickable), an outsource worker
  // ("ow-…" → 外包 代號 when resolvable, else the raw id; chat reachable either
  // way), and — since the id is chat-addressable regardless — always clickable.
  // reassignedFrom "" (never reassigned) → no row at all (null).
  const previousAssignee = ((): { text: string; peerId: string } | null => {
    const id = task.reassignedFrom ?? "";
    if (id === "") return null;
    if (task.reassignedFromKind === "outsource" || id.startsWith("ow-")) {
      const cn =
        workers.find((x) => x.id === id)?.codename ?? releasedCodenames.get(id);
      return { text: cn ? `${t.tasks.outsource} ${cn}` : id, peerId: id };
    }
    const m = members.find((x) => x.id === id);
    return { text: m ? m.name : id, peerId: id };
  })();

  // 負責人 == 建立者 → drop the 建立者 row entirely (owner 2026-07-17): it
  // restates the line directly above it, and the card's job is to say each
  // fact once.
  // ID comparison, NEVER the display name: two members can share a display
  // name, so a name match would hide a genuinely different creator — the one
  // failure mode of this feature that actively lies. `creator.text` is the
  // rendered name and is deliberately NOT consulted here.
  // The `!== ""` guard is load-bearing, not defensive noise: an unassigned
  // 外包 task has executorId "" and a pre-column task has creatorId "" — with
  // both empty they would compare EQUAL and this rule would eat the 「—」 row
  // that T-5012-shaped tasks legitimately show. Empty is not an identity.
  const creatorIsExecutor =
    task.creatorId !== "" && task.creatorId === task.executorId;

  // Collapsed by default (mockup): clicking anywhere on the card toggles
  // workflow + gates + description (owner mobile refactor 2026-07-17 — the
  // chevron button is gone; the whole card is the toggle surface). Plain
  // component state — never persisted. A §3.6 jump target auto-expands (the
  // jump promises visibility — a collapsed card would hide exactly what the
  // link came to show).
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    if (located) setExpanded(true);
  }, [located]);

  // 任務編號 chip 點擊複製(owner 2026-07-19 圈截圖):點 chip → 把顯示的任務
  // 編號(task.taskNo，非內部 id)寫進剪貼簿,給一個短暫「已複製」回饋。chip 本身
  // 是個 <button>,所以 onCardToggleClick 的 closest("button,…") 濾網會自動放行
  // (點它不會展開卡片),Enter/Space 由 button 原生觸發、也不會冒泡去 toggle 卡。
  // 只有真的寫入成功才亮「已複製」—— copyText 失敗回 false,絕不假成功。
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    },
    []
  );
  async function copyTaskNo() {
    const ok = await copyText(task.taskNo);
    if (!ok) return;
    setCopied(true);
    if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(false), 1600);
  }

  // 等我回覆 status-badge jump (v3): clicking the badge expands the card and,
  // once the hydrated workflow has rendered the embedded reply card, scrolls
  // it into view. The pending flag survives the async hydrate — the effect
  // below (after the hydrate wiring) fires on every detail/expand change
  // until the card exists.
  const [replyScrollPending, setReplyScrollPending] = useState(false);
  const rootRef = useRef<HTMLElement>(null);

  // Whole-card toggle with an interaction filter: clicks that land on (or
  // inside) any interactive element — the 狀態/優先權 dropdowns, the meta chips (chat /
  // type-settings / 識別鍵 links), the message composer (paperclip / textarea
  // / 送出), embedded reply-card controls, attachment thumbnails
  // (img[role=button] → Lightbox, T-5e8a), markdown links, modals — must do
  // their own job, never flip the card. closest() covers them all without
  // sprinkling stopPropagation over every child. The [role='button'] entry
  // ALSO matches the card itself (the article carries role=button), so a hit
  // only vetoes the toggle when it is NOT the card — otherwise every card-body
  // click would be swallowed (review fix on c891881). An in-progress text
  // selection (a drag that ends on the card) is not a toggle intent either.
  function onCardToggleClick(e: React.MouseEvent<HTMLElement>) {
    const target = e.target as HTMLElement;
    const hit = target.closest(
      "button, a, textarea, input, select, [role='button'], [role='menu'], [role='dialog']"
    );
    if (hit && hit !== e.currentTarget) return;
    // A non-collapsed selection at click time means the click ended a drag-
    // selection (a plain click collapses any prior selection first) — reading
    // is not a toggle intent. isCollapsed over toString(): jsdom implements
    // the former faithfully, and an all-whitespace drag still counts.
    const sel = window.getSelection();
    if (sel && sel.rangeCount > 0 && !sel.isCollapsed) return;
    setExpanded((v) => !v);
  }

  // Keyboard operability (repo convention for clickable surfaces — see the
  // chat header / MemberCard row: role="button" + tabIndex + Enter/Space).
  // Only keys on the card ITSELF toggle — an Enter bubbling out of the
  // message textarea (send) or any nested control must never flip the card.
  function onCardToggleKeyDown(e: React.KeyboardEvent<HTMLElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      setExpanded((v) => !v);
    }
  }

  // ── heavy detail (steps + description): the list is the LIGHT projection, so
  // the card hydrates its workflow timeline the first time it opens, keyed on
  // updatedTs so an SSE-driven refetch (reconcile-by-refetch) re-pulls a live
  // card's steps too. `detail` stays null while collapsed / before the first
  // load; `hydrating` gates the workflow loading placeholder (only shown when
  // there is nothing to render yet — a re-hydrate keeps the old timeline up).
  const [detail, setDetail] = useState<TaskView | null>(null);
  const [hydrating, setHydrating] = useState(false);
  const [hydrateError, setHydrateError] = useState(false);
  // Bumped by the retry affordance to re-run the effect after a failed load.
  const [retryNonce, setRetryNonce] = useState(0);
  useEffect(() => {
    // Collapsed: nothing is in flight — clear the spinner so a fetch abandoned
    // mid-flight by a collapse never leaves `hydrating` stuck true.
    if (!expanded) {
      setHydrating(false);
      return;
    }
    let alive = true;
    setHydrating(true);
    setHydrateError(false);
    onHydrate(task.id)
      .then((full) => {
        if (alive) setDetail(full);
      })
      .catch((e) => {
        if (alive) setHydrateError(true);
        console.warn("TaskCard: detail hydrate failed", e);
      })
      .finally(() => {
        if (alive) setHydrating(false);
      });
    return () => {
      alive = false;
    };
    // task.id guards against a recycled card; task.updatedTs re-pulls a live one;
    // retryNonce lets the error-state retry button force a fresh fetch.
  }, [expanded, task.id, task.updatedTs, retryNonce, onHydrate]);

  // The card renders heavy fields (steps/description) from the hydrated detail
  // when present, else from the light task (empty until the first load lands).
  // Its id must match the current row — a just-recycled card shows nothing
  // heavy until its own detail arrives.
  const hasDetail = detail !== null && detail.id === task.id;
  const view = hasDetail ? detail : task;
  // A task with no detail yet: show a loading placeholder while hydrating,
  // and an error+retry state if the fetch failed — never the transitional
  // empty state, which would be a silent lie. (The workflow render below ALSO
  // falls back to loading for progressTotal > 0 when hydrating is already
  // false — the pre-hydrate frame / a stepless resolve — so a leaf-bearing
  // task never flashes 「等待建立 Steps」; T-71e8.) The loading gate deliberately
  // does NOT require progressTotal > 0: superseded replan history is excluded
  // from the progress counts (T-1aea), so a task can honestly report 0/0 while
  // its detail still carries frozen steps to render — claiming 「等待建立
  // Steps」 during that first fetch would lie; a genuinely zero-step task just
  // shows one brief loading frame instead. Two carve-outs: 等待指派 derives
  // from the light row alone (executor projection — no detail needed), so an
  // unassigned card keeps its immediate transition; and the error gate keeps
  // the progressTotal > 0 requirement — with 0/0 the light list cannot tell
  // frozen history apart from no-plan-yet, and the zero-leaf transition copy
  // is the long-standing degraded fallback there.
  const detailPending = expanded && !hasDetail && hydrating && !unassigned;
  const detailFailed =
    expanded && !hasDetail && hydrateError && !hydrating && task.progressTotal > 0;

  // 等我回覆 badge jump (v3): once the expanded card has rendered its embedded
  // reply card (post-hydrate), scroll it into view and clear the pending flag.
  // The target is the WAITING gate's card, pinned by the step's replyCardId
  // through the data-reply-card-id attribute — a timeline can carry earlier
  // answered (collapsed) cards, and the DOM-first card would be the wrong one
  // (review fix). Fallbacks stay honest: no waiting step (badge raced a
  // refetch) → the first embedded card. Re-runs on every detail/hydrating
  // change so the async hydrate is covered; a collapse abandons the jump (the
  // promise was "show the card", and the user closed it).
  useEffect(() => {
    if (!replyScrollPending) return;
    if (!expanded) {
      setReplyScrollPending(false);
      return;
    }
    const waiting = view.steps.find(
      (s) => s.replyCardId !== "" && s.replyCardStatus === "waiting"
    );
    const cards = Array.from(
      rootRef.current?.querySelectorAll('[data-testid="task-reply-card"]') ??
        []
    );
    const el = waiting
      ? cards.find(
          (c) => c.getAttribute("data-reply-card-id") === waiting.replyCardId
        )
      : cards[0];
    if (el) {
      el.scrollIntoView({ behavior: "smooth", block: "center" });
      setReplyScrollPending(false);
    }
  }, [replyScrollPending, expanded, detail, hydrating, view.steps]);

  // ── owner actions (狀態 badge menu / terminate confirm) ────────────────────
  // 狀態 badge dropdown (v5, owner 2026-07-17): clicking the status badge ALWAYS
  // drops a menu — 標記重複 / 終止 (they used to live in a ⋮ menu, which owner
  // then had us delete outright once this move left it empty), plus the 等我回覆
  // jump as an extra item only while the task really is waiting on the owner.
  // The old one-click badge→jump is deliberately now a two-step (badge → 查看
  // 等我回覆卡): owner's informed ruling, not a regression.
  // ALWAYS means closed cards too (owner ruling 2026-07-17, spec ② follow-up,
  // rc-12d552eed7ce): 已完成/已終止/已標為重複 drop the same menu, with 標記重複
  // and 終止 greyed + disabled — the server 409s both on a closed task, so the
  // items are shown-but-dead rather than hidden.
  const [statusOpen, setStatusOpen] = useState(false);
  const statusRef = useRef<HTMLDivElement>(null);
  // In-place priority editing (v2/v3, owner 2026-07-17): clicking the
  // priority chip on the badge row drops a select-style menu right under it —
  // saved on pick, no card expand. Closed on pick / outside click.
  const [prioOpen, setPrioOpen] = useState(false);
  const prioRef = useRef<HTMLDivElement>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // 轉派 (T-160e): the reassign dialog owns its own draft/busy/error.
  const [reassignOpen, setReassignOpen] = useState(false);
  // Mark-duplicate (T-02c9): the picker modal + its selected original.
  const [dupOpen, setDupOpen] = useState(false);
  const [dupTarget, setDupTarget] = useState("");
  const [dupError, setDupError] = useState<string | null>(null);
  // Candidate originals: every OTHER task that is not itself a duplicate (the
  // server rejects pointing at a duplicate — pre-filter for a clean picker).
  const dupCandidates = allTasks.filter(
    (x) => x.id !== task.id && x.status !== "duplicated"
  );

  useEffect(() => {
    if (!prioOpen) return;
    function onDown(e: MouseEvent) {
      if (!prioRef.current?.contains(e.target as Node)) setPrioOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [prioOpen]);

  useEffect(() => {
    if (!statusOpen) return;
    function onDown(e: MouseEvent) {
      if (!statusRef.current?.contains(e.target as Node)) setStatusOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [statusOpen]);

  async function doSetPriority(priority: string) {
    setPrioOpen(false);
    if (priority === task.priority) return;
    try {
      await onSetPriority(task.id, priority);
      setActionError(null);
    } catch (e) {
      console.warn("TaskCard: priority change failed", e);
      setActionError(t.tasks.actionError);
    }
  }

  async function doTerminate() {
    setBusy(true);
    try {
      await onTerminate(task.id);
      setConfirmOpen(false);
      setActionError(null);
    } catch (e) {
      console.warn("TaskCard: terminate failed", e);
      setActionError(t.tasks.actionError);
      setConfirmOpen(false);
    } finally {
      setBusy(false);
    }
  }

  async function doMarkDuplicate() {
    if (!dupTarget) {
      setDupError(t.tasks.markDuplicatePick);
      return;
    }
    setBusy(true);
    try {
      await onMarkDuplicate(task.id, dupTarget);
      setDupOpen(false);
      setDupTarget("");
      setDupError(null);
      setActionError(null);
    } catch (e) {
      console.warn("TaskCard: mark duplicate failed", e);
      setDupError(t.tasks.actionError);
    } finally {
      setBusy(false);
    }
  }

  // ── message box (owner → executor) ─────────────────────────────────────────
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [msgError, setMsgError] = useState(false);
  const isComposingRef = useRef(false);
  const draftRef = useRef<HTMLTextAreaElement>(null);
  // Attachment staging — the SHARED useAttachmentStaging state machine (same
  // caps + funnels as the chat composer / ReplyComposer): paste an image into
  // the textarea or pick files via the paperclip; all staged items ride the
  // SAME message (POST /api/tasks/{id}/message already carries attachments).
  const {
    pendingAttachments,
    attachError,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  } = useAttachmentStaging();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const canSend =
    !sending &&
    !unassigned &&
    (draft.trim().length > 0 || pendingAttachments.length > 0);

  // Multi-line message box (Enter sends, Shift+Enter breaks a line — same as
  // the chat composer / ReplyComposer): auto-grow the textarea to the draft so
  // a long message is fully visible while being typed; the CSS max-height caps
  // it and the textarea scrolls beyond.
  useLayoutEffect(() => {
    if (draftRef.current) autosizeTextarea(draftRef.current);
  }, [draft]);

  async function sendMessage() {
    if (!canSend) return;
    const attachments: ChatAttachmentInput[] = pendingAttachments.map((a) => ({
      dataB64: a.dataUri,
      ...(a.filename ? { filename: a.filename } : {}),
      mime: a.mime,
    }));
    setSending(true);
    try {
      await onSendMessage(task.id, draft.trim(), attachments);
      setDraft("");
      clearAttachments();
      setMsgError(false);
    } catch (e) {
      console.warn("TaskCard: message send failed", e);
      setMsgError(true); // the typed content is kept — retry-friendly
    } finally {
      setSending(false);
    }
  }

  function onMsgKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (
      e.nativeEvent.isComposing ||
      e.keyCode === 229 ||
      isComposingRef.current
    ) {
      return; // IME guard — an Enter confirming a CJK candidate never sends
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void sendMessage();
    }
  }

  // ── embedded reply cards ────────────────────────────────────────────────────
  // Each step carrying a card (non-empty replyCardId — an armed gate OR an
  // auto-bound plain ask) embeds one TaskReplyCard INSIDE its step row.

  // ── header stamps ──────────────────────────────────────────────────────────
  // 已歷時 ticks from created_ts (含 等待指派/等待建立 Steps — spec §3.1); a CLOSED
  // task freezes at closed_ts. created_ts 0 (no real stamp) → honest "—".
  const elapsedEnd = task.closedTs ?? nowTs;
  const elapsedText =
    task.createdTs > 0 ? formatDuration(elapsedEnd - task.createdTs) : "—";
  const progressPct =
    task.progressTotal > 0
      ? Math.min(100, (task.progressDone / task.progressTotal) * 100)
      : 0;

  // ── step badge — resolveStepBadge (lib/stepBadge.ts) is the ONE decision
  // source: 等我回覆 renders only while the bound card really WAITS; an
  // answered/expired card shows its own badge even if the step is still held
  // at waiting_owner (T-d64f).
  function renderStepBadge(step: TaskStepView) {
    const badge = resolveStepBadge(step);
    switch (badge.kind) {
      case "gate-announced":
        return (
          <span
            className="task-step-badge task-step-badge--gate-announced"
            data-testid="gate-announced"
          >
            {t.tasks.gateAnnounced}
          </span>
        );
      case "card-answered":
        return (
          <span
            className="task-step-badge task-step-badge--card-answered"
            data-testid="step-card-answered"
          >
            {t.tasks.stepCardAnswered}
          </span>
        );
      case "card-expired":
        return (
          <span
            className="task-step-badge task-step-badge--card-expired"
            data-testid="step-card-expired"
          >
            {t.tasks.stepCardExpired}
          </span>
        );
      case "waiting-external":
        return (
          <span
            className="task-step-badge task-step-badge--waiting-external"
            data-testid="step-waiting-external"
          >
            {t.tasks.stepWaitingExternal}
          </span>
        );
      default:
        return (
          <span className={`task-step-badge task-step-badge--${badge.status}`}>
            {/* Map miss → show the raw key DELIBERATELY (honest, debuggable —
             * never a blank badge). This branch must stay unreachable for the
             * closed set: stepBadge.i18n.test.ts enumerates STEP_STATUSES ×
             * every locale and goes red the moment a status lacks its
             * translations (T-6f11), so the raw key can only surface for a
             * value the whole frontend doesn't know yet. */}
            {t.tasks.stepStatus[badge.status] ?? badge.status}
          </span>
        );
    }
  }

  // Step 耗時 — only from REAL stamps: done = finished-started; running/waiting
  // = now-started; pending (no start) renders nothing (never fabricated).
  function stepDuration(step: TaskStepView): string | null {
    if (step.startedTs <= 0) return null;
    if (step.finishedTs > 0) {
      return formatDuration(step.finishedTs - step.startedTs);
    }
    if (step.status === "in_progress" || step.status === "waiting_owner") {
      return formatDuration(nowTs - step.startedTs);
    }
    return null;
  }

  function renderStep(step: TaskStepView) {
    const dur = stepDuration(step);
    // The PERMANENT approval marker: a step that is a declared gate OR ever
    // had a reply card bound (the pointer persists after the step finishes)
    // stays recognisable as an approval step forever (owner 2026-07-14).
    const everApproval = step.isGate || step.replyCardId !== "";
    return (
      <div
        key={step.id}
        className={`task-step task-step--${step.status}`}
        data-testid="task-step"
      >
        <div className="task-step__row">
          <span className="task-step__name">{step.name}</span>
          {everApproval && (
            <span className="task-step__gate-mark" data-testid="gate-mark">
              {t.tasks.gateMark}
            </span>
          )}
          {dur && (
            <span className="task-step__duration">
              <ClockIcon size={12} /> {dur}
            </span>
          )}
          {renderStepBadge(step)}
        </div>
        {step.dod && (
          <div className="task-step__dod">
            <span className="task-step__dod-label">{t.tasks.dod}</span>
            <Markdown source={step.dod} className="doc-md" />
          </div>
        )}
        {/* 等待外部 reason (T-9ca5): the step's own one-line reason, mirrored
            up to the task's waiting block. Agent free text → <Markdown>; label
            on its own line (always-stack, same rule as the task waiting row). */}
        {step.status === "waiting_external" && step.waitingReason && (
          <div className="task-step__waiting" data-testid="step-waiting-reason">
            <ClockIcon size={12} />
            <span className="task-step__waiting-label">
              {t.tasks.waitingLabel}
            </span>
            <Markdown
              source={step.waitingReason}
              className="task-step__waiting-md doc-md"
            />
          </div>
        )}
        {/* The step's reply card lives IN the step (owner 2026-07-14) — the
         * whole timeline only renders while the card is expanded, so no extra
         * gating here. Answered cards collapse inside TaskReplyCard. */}
        {step.replyCardId !== "" && (
          <TaskReplyCard
            key={step.replyCardId}
            replyCardId={step.replyCardId}
            initialStatus={step.replyCardStatus}
            fallbackSummary={step.name}
          />
        )}
      </div>
    );
  }

  const timeline = buildTimeline(view.steps);

  return (
    <article
      ref={rootRef}
      className={`task-card task-card--${task.status}${
        located ? " task-card--located" : ""
      }`}
      data-testid="task-card"
      data-task-id={task.id}
      role="button"
      tabIndex={0}
      aria-expanded={expanded}
      aria-label={expanded ? t.tasks.collapseCard : t.tasks.expandCard}
      onClick={onCardToggleClick}
      onKeyDown={onCardToggleKeyDown}
    >
      {/* ── head (v4 layout — owner 2026-07-17「這兩排應該要對調」): the v2
           order SWAPPED. Row 1 is the badge row (☑ #T-xxxx id badge · 優先權
           chip (in-place editable) · 狀態 badge) and row 2 the title; the ▸/▾
           expand indicator holds the card's top-right corner, riding row 1
           alongside the badges — the corner is the anchor, not the title
           (T-70fb behaviour 1, now served by the triangle instead of the
           deleted ⋮). No avatar; no chevron — the whole card toggles. ── */}
      <header className="task-card__head">
        <div className="task-card__head-top">
          {/* Row 1 — the fixed badge row: #T-xxxx · 優先權 · 狀態 (v3 order —
              the id leads). The priority chip edits in place (owner v2/v3):
              clicking it drops a vertical select-style menu (same visual
              vocabulary as the 狀態 dropdown popover); picking one calls
              setTaskPriority — the card never has to expand. A closed task's
              priority is frozen history → plain span. A 等我回覆 status badge
              is itself a jump: click → expand the card + scroll to the embedded
              reply card (v3); every other status is a plain badge. */}
          <div className="task-card__badge-row">
            <button
              type="button"
              className="task-card__id-badge task-card__id-badge--copy"
              data-testid="task-no"
              aria-label={t.tasks.copyTaskNo(task.taskNo)}
              title={t.tasks.copyTaskNo(task.taskNo)}
              onClick={copyTaskNo}
            >
              {copied ? <CheckIcon size={13} /> : <TasksIcon size={13} />}#
              {task.taskNo}
              {copied && (
                <span
                  className="task-card__id-badge-copied"
                  role="status"
                  data-testid="task-no-copied"
                >
                  {t.tasks.taskNoCopied}
                </span>
              )}
            </button>
            {closed ? (
              <span
                className={`task-badge task-badge--prio-${task.priority}`}
                data-testid="task-priority"
              >
                {t.tasks.priority[task.priority] ?? task.priority}
              </span>
            ) : (
              <div className="task-card__prio" ref={prioRef}>
                <button
                  type="button"
                  className={`task-badge task-badge--prio-${task.priority} task-card__prio-chip`}
                  title={t.tasks.priorityLabel}
                  aria-label={t.tasks.priorityLabel}
                  aria-haspopup="menu"
                  aria-expanded={prioOpen}
                  data-testid="task-priority"
                  onClick={() => setPrioOpen((o) => !o)}
                >
                  {t.tasks.priority[task.priority] ?? task.priority}
                </button>
                {prioOpen && (
                  <div
                    className="task-card__menu-pop task-card__prio-pop"
                    role="menu"
                    aria-label={t.tasks.priorityLabel}
                    data-testid="task-priority-options"
                  >
                    {PRIORITIES.map((p) => (
                      <button
                        key={p}
                        type="button"
                        role="menuitem"
                        className={`task-card__menu-item${
                          task.priority === p
                            ? " task-card__menu-item--active"
                            : ""
                        }`}
                        data-testid={`priority-${p}`}
                        onClick={() => void doSetPriority(p)}
                      >
                        {t.tasks.priority[p]}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            <div className="task-card__status" ref={statusRef}>
              <button
                type="button"
                className={`task-badge task-badge--status-${task.status} task-card__status-chip`}
                title={t.tasks.statusMenuLabel}
                aria-label={t.tasks.statusMenuLabel}
                aria-haspopup="menu"
                aria-expanded={statusOpen}
                data-testid="task-status"
                onClick={() => setStatusOpen((o) => !o)}
              >
                {t.tasks.status[task.status] ?? task.status}
              </button>
              {statusOpen && (
                <div
                  className="task-card__menu-pop task-card__status-pop"
                  role="menu"
                  aria-label={t.tasks.statusMenuLabel}
                  data-testid="task-status-options"
                >
                  {/* 標記重複 / 終止 are non-terminal-only — the server 409s both
                      on a closed task (已完成/已終止/已標為重複). Owner's ruling
                      (2026-07-17, spec ② follow-up): the badge drops the menu
                      ALWAYS, and on a closed card these two render greyed and
                      unclickable rather than firing a request that can only
                      fail. `disabled` is the real gate (no onClick fires); the
                      class is only the grey. */}
                  <button
                    type="button"
                    role="menuitem"
                    className={`task-card__menu-item${
                      closed ? " task-card__menu-item--disabled" : ""
                    }`}
                    data-testid="task-mark-duplicate"
                    disabled={closed}
                    aria-disabled={closed}
                    onClick={() => {
                      setStatusOpen(false);
                      setDupError(null);
                      setDupTarget("");
                      setDupOpen(true);
                    }}
                  >
                    {t.tasks.markDuplicate}
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    className={`task-card__menu-item task-card__menu-item--danger${
                      closed ? " task-card__menu-item--disabled" : ""
                    }`}
                    data-testid="task-terminate"
                    disabled={closed}
                    aria-disabled={closed}
                    onClick={() => {
                      setStatusOpen(false);
                      setConfirmOpen(true);
                    }}
                  >
                    {t.tasks.terminate}
                  </button>
                  {/* The 等我回覆 jump — an EXTRA item, present only while the
                      task really waits on the owner (v5: the v3 one-click
                      badge jump folded into this menu). A closed task can never
                      be 等我回覆, so this never pairs with the greyed items. */}
                  {task.status === "waiting_owner" && (
                    <button
                      type="button"
                      role="menuitem"
                      className="task-card__menu-item"
                      data-testid="task-status-jump"
                      onClick={() => {
                        setStatusOpen(false);
                        setExpanded(true);
                        setReplyScrollPending(true);
                      }}
                    >
                      {t.tasks.statusJump}
                    </button>
                  )}
                </div>
              )}
            </div>

            {/* 轉派中 LOCK overlay (T-9ca5): ORTHOGONAL to status — a reassigned
                task keeps its honest derived status badge AND shows this while
                task.lock === "reassigning" (never a status value any more). */}
            {task.lock === "reassigning" && (
              <span
                className="task-badge task-badge--lock-reassigning"
                data-testid="task-lock"
              >
                {t.tasks.lockReassigning}
              </span>
            )}

            {/* 任務類型 — row 1, a COLOURED .task-badge, same vocabulary as the
                優先權/狀態 badges beside it (owner picked 候選 A, 2026-07-17).
                It used to be a neutral .task-card__chip (4% white fill) down in
                the .task-card__meta label grid, which read as a third-class
                field while type is in fact one of the card's identifying
                facts. Its own teal (--color-task-type) — NOT --color-accent —
                because in_progress is already accent green and two same-
                coloured badges side by side would read as one status pair.
                The 齒輪 deep-link into the type's settings hub is unchanged;
                only the styling and the slot moved.
                390px: the badge row is flex-wrap, so a type name that does not
                fit WRAPS to a second line rather than truncating. Owner ruled
                on this explicitly (「手機上 type 自己一行我接受」) after seeing
                the 候選 B truncation measured: cutting to 6 chars +「…」 STILL
                wrapped at 390px, so truncation bought nothing and cost the
                name. Wrapping is the accepted outcome, not a bug to fix. */}
            {typeClickable ? (
              <button
                type="button"
                className="task-badge task-badge--type task-card__type-chip"
                title={t.tasks.typeSettingsLink}
                data-testid="task-type-link"
                onClick={openTypeSettings}
              >
                <GearIcon size={13} />
                <span className="task-card__type" data-testid="task-type">
                  {typeLabel}
                </span>
              </button>
            ) : (
              <span
                className="task-badge task-badge--type"
                data-testid="task-type-row"
              >
                <span className="task-card__type" data-testid="task-type">
                  {typeLabel}
                </span>
              </span>
            )}
            {/* 產物 N — the artifact-set count badge (T-3dc5, owner design A):
                the rightmost coloured badge in this row; renders ONLY when the
                count > 0, and clicking opens the tabbed popover. It deliberately
                does NOT touch the top-right chevron slot (spec ③). */}
            <TaskArtifactsBadge
              task={view}
              onHydrate={onHydrate}
              onRemoveArtifact={onRemoveArtifact}
            />
          </div>

          {/* ── expand indicator (v6, owner 2026-07-17): a ChevronRight
               (collapsed) / ChevronDown (expanded) ICON at size=18 — the same
               component + size the settings page uses in all 8 of its
               drill-in rows (SettingsPage.tsx:362/373/387/403/1125/1135/1145/
               1183). v5's text glyphs (▸/▾) rendered at 5.27×11px — 1/15 the
               area of the settings chevron and a different shape, so the same
               "expand" affordance spoke two visual languages across the app
               (T-bc90). Icon, not glyph: the font's idea of ▸ is not ours.
               It still OWNS the card's top-right corner: the ⋮ menu is
               gone (owner ruling — spec ② emptied it by moving 標記重複/終止 to
               the 狀態 badge dropdown, and an empty menu button is worse than
               none). T-70fb behaviour 1 asked for a STABLE top-right anchor,
               not for that particular button — the triangle inherits the slot,
               so the corner never goes empty and the head row keeps its shape,
               closed cards included (the ⋮ used to vanish on !closed; this
               does not).
               A pure STATE INDICATOR, never a control — the whole card is
               still the toggle surface (T-70fb behaviour 3), so this is a
               plain aria-hidden span with NO role/button: a click on it falls
               straight through the closest() interaction filter to the card
               toggle, exactly like a click on the title. aria-expanded on the
               card article already carries this state to AT. ── */}
          <span
            className="task-card__expand-mark"
            data-testid="task-expand-mark"
            aria-hidden="true"
          >
            {expanded ? <ChevronDownIcon size={18} /> : <ChevronRightIcon size={18} />}
          </span>
        </div>

        {/* Row 2 — the title. Demoted under the badges (v4); still the h3,
            still the card's heading in the a11y tree. */}
        <div className="task-card__title-line">
          <h3 className="task-card__title">{task.title}</h3>
        </div>

        {/* Below the badge row: an aligned label column (任務類型 / 負責人 /
            建立者 / 識別鍵) — task_no left the stack for the badge row (v2)
            (owner spec 2026-07-15). A CSS grid gives every label cell the same
            max-content width so the value chips line up across rows and across
            languages. Each row emits two grid children: the muted label span
            and the value chip. The chip — not the label — carries the icon and
            the click, so the jump target sits inside the pill (spec):
              類型 chip (齒輪) → #settings/manuals/<typeKey>
              負責人/建立者 chip (聊天泡泡) → 該對象聊天 compose. */}
        <div className="task-card__meta">
          {/* 任務類型 left this grid for the badge row (v6) — see the head. */}

          {/* 負責人 (聊天泡泡 in the chip when reachable) + 轉派 icon to its right
              (owner 2026-07-18): 轉派 is a hand-the-task-to-a-new-executor
              action, so it belongs beside WHO owns the task. The chip and the
              icon share the grid's value cell through .task-card__assignee-cell
              (inline-flex): the chip stays shrinkable so a long 外包
              代號·模型·投入度 line still ellipses, and the icon is flex:none, so
              at 390px the name wraps/ellipses but the button never clips. The
              button is a real interactive element (closest() in the card-toggle
              filter already excludes it), shown only while the task is
              non-terminal — the server 409s a reassign on a closed task. */}
          <span className="task-card__meta-label">{t.tasks.assigneeLabel}</span>
          <div className="task-card__assignee-cell">
            {assigneePeerId ? (
              <button
                type="button"
                className="task-card__chip task-card__chip--link"
                title={t.tasks.messageAssignee}
                aria-label={t.tasks.messageAssignee}
                data-testid="task-assignee-link"
                onClick={() => openChat(assigneePeerId)}
              >
                <span className="task-card__executor" data-testid="task-executor">
                  {executorText}
                </span>
                <ChatBubbleIcon size={13} />
              </button>
            ) : (
              <span className="task-card__chip" data-testid="task-assignee-row">
                <span className="task-card__executor" data-testid="task-executor">
                  {executorText}
                </span>
              </span>
            )}
            {!closed && (
              <button
                type="button"
                className="task-card__reassign"
                title={t.tasks.reassign}
                aria-label={t.tasks.reassign}
                data-testid="task-reassign"
                onClick={() => setReassignOpen(true)}
              >
                <SwapIcon size={14} />
              </button>
            )}
          </div>

          {/* 建立者 — member/外包 可點(聊天泡泡); ""→— 與 未知值 純文字(無圖示).
              Hidden entirely when the creator IS the assignee (v6, owner
              2026-07-17): the row would restate the line directly above it. */}
          {!creatorIsExecutor && (
            <>
              <span className="task-card__meta-label">{t.tasks.creatorLabel}</span>
              {creator.peerId ? (
                <button
                  type="button"
                  className="task-card__chip task-card__chip--link"
                  title={t.tasks.messageCreator}
                  aria-label={t.tasks.messageCreator}
                  data-testid="task-creator-link"
                  onClick={() => openChat(creator.peerId)}
                >
                  <span className="task-card__creator" data-testid="task-creator">
                    {creator.text}
                  </span>
                  <ChatBubbleIcon size={13} />
                </button>
              ) : (
                <span className="task-card__chip" data-testid="task-creator-row">
                  <span className="task-card__creator" data-testid="task-creator">
                    {creator.text}
                  </span>
                </span>
              )}
            </>
          )}

          {/* 前任 (T-ba04 轉派交接) — the predecessor to hand over WITH; shown
              only while/after a reassign stamped one (reassignedFrom !== "").
              Always a chat-reachable chip (the id is addressable regardless of
              whether the peer still resolves to a live roster/worker row). */}
          {previousAssignee && (
            <>
              <span className="task-card__meta-label">
                {t.tasks.previousAssigneeLabel}
              </span>
              <button
                type="button"
                className="task-card__chip task-card__chip--link"
                title={t.tasks.messagePreviousAssignee}
                aria-label={t.tasks.messagePreviousAssignee}
                data-testid="task-previous-assignee-link"
                onClick={() => openChat(previousAssignee.peerId)}
              >
                <span
                  className="task-card__creator"
                  data-testid="task-previous-assignee"
                >
                  {previousAssignee.text}
                </span>
                <ChatBubbleIcon size={13} />
              </button>
            </>
          )}

          {/* 識別鍵 — URL 值可外連(chip 內帶外連圖示); 空值整列不顯示 (ad-hoc) */}
          {task.dedupeKey && (
            <>
              <span className="task-card__meta-label">{t.tasks.keyLabel}</span>
              {isUrl(task.dedupeKey) ? (
                <a
                  className="task-card__chip task-card__chip--link task-card__key-chip"
                  href={task.dedupeKey}
                  target="_blank"
                  rel="noreferrer"
                  title={t.tasks.openKeyLink}
                  data-testid="task-key-link"
                >
                  <span className="task-card__key">{task.dedupeKey}</span>
                  <ExternalLinkIcon size={11} />
                </a>
              ) : (
                <span
                  className="task-card__chip task-card__key-chip"
                  data-testid="task-key-row"
                >
                  <span className="task-card__key" data-testid="task-key">
                    {task.dedupeKey}
                  </span>
                </span>
              )}
            </>
          )}
        </div>
      </header>

      {/* ── deps 「等 T-xxxx」 (被 T-xxxx 擋住) — v6 (owner 2026-07-17).
           Was a bare .task-key--dep: a grey hairline box of mono text with no
           icon, wedged between the 建立者 row and the progress bar, reading as
           debug output rather than as "this task is waiting on something".
           Now it borrows the waiting_external block's LOWER-HALF vocabulary —
           ClockIcon size=13 + a rounded tinted block — because that is already
           the card's word for "waiting", and saying the same thing two ways
           is what made this unreadable.
           BLUE (#8fb6d9, the colour .task-key--dep already carried), never
           waiting_external's purple: same shape + same colour would assert
           「這張卡是 waiting_external 狀態」, which is false — a dep block is
           orthogonal to status.
           🔴 It is deliberately NOT a row-1 badge. deps are NOT a status: a
           card can be in_progress AND blocked by T-70fb at the same time (the
           work is still running; the dep is a note). Standing it beside the
           real 狀態 badge makes the card claim two statuses and read as
           stalled when it is not — owner saw that rendered (候選 C) and did
           not pick it. Keep it out of the badge row. */}
      {task.deps.length > 0 && (
        <div className="task-card__deps">
          {task.deps.map((depId) => {
            const dep = allTasks.find((x) => x.id === depId);
            return (
              <div
                key={depId}
                className="task-card__waiting task-card__waiting--dep"
                data-testid="task-dep"
              >
                <ClockIcon size={13} />
                <span>{t.tasks.blockedBy(dep?.taskNo ?? depId)}</span>
              </div>
            );
          })}
        </div>
      )}

      {/* ── progress + elapsed ── */}
      <div className="task-card__progress">
        <div className="task-card__progress-bar">
          <div
            className={`task-card__progress-fill task-card__progress-fill--${task.status}`}
            style={{ width: `${progressPct}%` }}
          />
        </div>
        <span className="task-card__progress-text" data-testid="task-progress">
          {t.tasks.progress(task.progressDone, task.progressTotal)} ·{" "}
          {t.tasks.elapsed(elapsedText)}
        </span>
      </div>

      {/* ── waiting-external reason (T-a20b) — waitingReason is agent-authored
           free text (owner's screenshot had `**fms #20054**` / `` `919fe961` ``
           landing as literal asterisks/backticks), so it renders through the
           shared Markdown component. The 等待中 label stays OUTSIDE the
           <Markdown> — feeding the whole i18n template in would hand the
           prefix to the markdown parser.

           The label carries NO trailing "·" separator: the row puts the markdown
           on its own line at EVERY width (tasks.css always-stack), and a
           separator whose job is to join "label · reason" on one line becomes an
           orphan dangling at the end of the label's line the moment they stop
           sharing one. The label reads as a heading for the block below it
           instead. (This held even when the stack was mobile-only — owner's
           2026-07-17 rc-91492e026e87 ruling made it unconditional, which only
           makes the separator MORE orphaned, never less.)
           Note this is a JSX literal, NOT part of the i18n string — the
           `waitingLabel` entries are bare words ("等待中"/"Waiting"/"候緣"), so
           no locale needed touching. ── */}
      {task.status === "waiting_external" && task.waitingReason && (
        <div className="task-card__waiting" data-testid="task-waiting-reason">
          <ClockIcon size={13} />
          <span className="task-card__waiting-label">{t.tasks.waitingLabel}</span>
          <Markdown
            source={task.waitingReason}
            className="task-card__waiting-md doc-md"
          />
        </div>
      )}

      {/* ── duplicated: link to the original it duplicates (§3.6 jump, depth-1
           so this always resolves in one hop) ── */}
      {task.status === "duplicated" && task.duplicateOf && (
        <div className="task-card__duplicate" data-testid="task-duplicate-of">
          <button
            type="button"
            className="task-card__chip task-card__chip--link"
            title={t.tasks.duplicateJump}
            data-testid="task-duplicate-link"
            onClick={() =>
              navigateHash({ page: "tasks", taskId: task.duplicateOf })
            }
          >
            <span>
              {t.tasks.duplicateOf(
                allTasks.find((x) => x.id === task.duplicateOf)?.taskNo ??
                  task.duplicateOf
              )}
            </span>
            <ExternalLinkIcon size={11} />
          </button>
        </div>
      )}

      {actionError && <div className="task-card__error">{actionError}</div>}

      {/* ── message box (owner → executor) ── */}
      {/* Staged-attachment preview strip — same markup/classes as the chat
       * composer / ReplyComposer (office.css, already imported by TasksPage):
       * one visual language for "composing a message". */}
      {(pendingAttachments.length > 0 || attachError) && (
        <ComposerAttachmentPreview
          pendingAttachments={pendingAttachments}
          attachError={attachError}
          onRemove={removeAttachment}
        />
      )}
      <div className="task-card__composer">
        <input
          ref={fileInputRef}
          className="chat__file-input"
          type="file"
          accept={ATTACH_ACCEPT}
          multiple
          onChange={onPickFile}
          hidden
        />
        <button
          type="button"
          className="chat__attach"
          aria-label={t.chat.attachLabel}
          title={t.chat.attachLabel}
          disabled={unassigned}
          data-testid="task-msg-attach"
          onClick={() => fileInputRef.current?.click()}
        >
          <PaperclipIcon size={18} />
        </button>
        {/* Multi-line message box: a bare Enter submits (onMsgKeyDown), a
         * shifted Enter falls through to the textarea's native newline. */}
        <textarea
          ref={draftRef}
          className="chat__input"
          rows={1}
          value={draft}
          disabled={unassigned || sending}
          placeholder={t.tasks.messagePlaceholder(
            unassigned
              ? t.tasks.unassigned
              : task.executorKind === "member"
                ? member?.name || task.executorId
                : worker
                  ? `${t.tasks.outsource} ${worker.codename}`
                  : t.tasks.outsource
          )}
          data-testid="task-msg-input"
          onChange={(e) => setDraft(e.target.value)}
          onCompositionStart={() => {
            isComposingRef.current = true;
          }}
          onCompositionEnd={(e) => {
            isComposingRef.current = false;
            setDraft(e.currentTarget.value);
          }}
          onKeyDown={onMsgKeyDown}
          onPaste={onPaste}
        ></textarea>
        <button
          type="button"
          className="task-card__send"
          disabled={!canSend}
          data-testid="task-msg-send"
          onClick={() => void sendMessage()}
        >
          <SendIcon size={14} />
          {t.tasks.send}
        </button>
      </div>
      {msgError && <div className="task-card__error">{t.tasks.messageError}</div>}

      {/* ── description (v5, owner 2026-07-17「task details 應該在回覆訊息的
           下面」): the 詳細敘述 sits BELOW the 傳訊息給 ○○… composer — the
           owner's "回覆訊息" is the message box, and his ruling moved ONLY this
           block (the meta column 任務類型/負責人/建立者 stays up in the head; the
           workflow's embedded reply cards are untouched). Placed after the
           composer's own msgError so the send-failure notice stays attached to
           the box it belongs to, and before the workflow so the timeline keeps
           the card's tail. Still EXPANDED-ONLY — the collapsed card stays
           compact — and still hydrated from the full task, so it appears once
           detail lands. ── */}
      {expanded && view.description && (
        <Markdown
          source={view.description}
          className="task-card__desc doc-md"
        />
      )}

      {/* ── workflow timeline / transitional states (expanded only) ──
       * Reply cards render INSIDE their step rows (renderStep). Legacy cards
       * are discovered the same way — through the step's replyCardId pointer —
       * so pre-binding data degrades to exactly the old surface, never a
       * crash. */}
      {expanded && (
      <div className="task-card__workflow">
        <div className="task-card__workflow-title">{t.tasks.workflow}</div>
        {detailFailed ? (
          // The hydrate failed — say so and offer a retry (re-clicking re-runs
          // the fetch) instead of silently rendering a fake 規劃中 empty state.
          <div className="task-card__transition task-card__transition--error"
               data-testid="task-steps-error">
            <span>{t.tasks.stepsLoadError}</span>
            <button
              type="button"
              className="task-card__retry"
              data-testid="task-steps-retry"
              onClick={() => setRetryNonce((n) => n + 1)}
            >
              {t.tasks.stepsRetry}
            </button>
          </div>
        ) : detailPending ? (
          // The heavy steps live on the full task, not the light list — show a
          // loading placeholder while the first hydrate is in flight so a task
          // that HAS a plan never flashes the "no steps yet" transition state.
          <div className="task-card__transition" data-testid="task-steps-loading">
            {t.tasks.stepsLoading}
          </div>
        ) : view.steps.length === 0 ? (
          unassigned ? (
            // 外包未指派 — 文案不變。
            <div className="task-card__transition" data-testid="task-transition">
              {t.tasks.waitingAssign}
            </div>
          ) : task.progressTotal > 0 ? (
            // The light list already counted leaves (progressTotal > 0) but the
            // hydrated detail carries none yet — the pre-hydrate frame (the
            // hydrate effect runs after paint, and a high-frequency SSE churn can
            // starve it) or a stepless getTask resolve. Show the loading
            // placeholder, NOT the transitional copy: progress says there IS a
            // plan, so claiming 「等待建立 Steps」 would be a lie (T-71e8). This
            // realigns the empty branch with detailPending/detailFailed, which
            // already gate on progressTotal > 0.
            <div className="task-card__transition" data-testid="task-steps-loading">
              {t.tasks.stepsLoading}
            </div>
          ) : (
            // Genuinely no leaves yet — assigned but the executor has not created
            // any steps. 「等待 ○○ 建立 Steps」, ○○ = the executor's display name
            // (member name / 外包 代號; owner ruling 2026-07). Resolved the same
            // way as the message-box placeholder above.
            <div className="task-card__transition" data-testid="task-transition">
              {t.tasks.planningBy(
                task.executorKind === "member"
                  ? member?.name || task.executorId
                  : worker?.codename || t.tasks.outsource
              )}
            </div>
          )
        ) : (
          <div className="task-timeline">
            {timeline.map((row) =>
              row.kind === "single" ? (
                <div key={row.step.id} className="task-timeline__row">
                  <span
                    className={`task-timeline__dot task-timeline__dot--${row.step.status}`}
                  />
                  {renderStep(row.step)}
                </div>
              ) : (
                // Key on the first leaf, not the group name: legacy data may
                // hold the SAME group split into two stages (submit_plan now
                // refuses new ones, stored rows are never rewritten) and two
                // rows must not collide on one React key.
                <div key={row.steps[0].id} className="task-timeline__row">
                  <span
                    className={`task-timeline__dot task-timeline__dot--${
                      row.steps.some((s) => s.status === "in_progress")
                        ? "in_progress"
                        : row.steps.every((s) => s.status === "done")
                          ? "done"
                          : "pending"
                    }`}
                  />
                  <div className="task-parallel" data-testid="task-parallel">
                    <div className="task-parallel__label">
                      ‖ {t.tasks.parallel(row.steps.length)}
                    </div>
                    {row.steps.map(renderStep)}
                  </div>
                </div>
              )
            )}
          </div>
        )}
      </div>
      )}

      {confirmOpen && (
        <ConfirmModal
          testId="terminate-confirm"
          confirmTestId="terminate-confirm-btn"
          body={t.tasks.terminateConfirmBody(task.title)}
          cancelLabel={t.common.cancel}
          confirmLabel={t.tasks.terminateConfirm}
          busy={busy}
          danger
          onCancel={() => setConfirmOpen(false)}
          onConfirm={() => void doTerminate()}
        />
      )}

      {reassignOpen && (
        <TaskReassignDialog
          task={task}
          members={members}
          onReassign={onReassign}
          onClose={() => setReassignOpen(false)}
        />
      )}

      {dupOpen && (
        <ConfirmModal
          testId="mark-duplicate"
          confirmTestId="mark-duplicate-btn"
          body={
            <div className="task-card__dup-picker">
              <div>{t.tasks.markDuplicateBody(task.taskNo)}</div>
              <select
                className="task-card__dup-select"
                data-testid="mark-duplicate-select"
                value={dupTarget}
                onChange={(e) => {
                  setDupTarget(e.target.value);
                  setDupError(null);
                }}
              >
                <option value="">{t.tasks.markDuplicatePick}</option>
                {dupCandidates.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.taskNo} · {c.title}
                  </option>
                ))}
              </select>
            </div>
          }
          error={dupError}
          cancelLabel={t.common.cancel}
          confirmLabel={t.tasks.markDuplicateConfirm}
          busy={busy}
          onCancel={() => {
            setDupOpen(false);
            setDupError(null);
          }}
          onConfirm={() => void doMarkDuplicate()}
        />
      )}
    </article>
  );
}
