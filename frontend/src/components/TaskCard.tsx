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
import { deriveTaskNo } from "../lib/taskNo";
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
  depsResolvable = true,
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
  /** Whether `allTasks` is the FULL population, so an unresolved dep id can be
   * called 查無此任務 honestly (T-1d82). False while the list is still the
   * open-only fast path: a dep that merely CLOSED is absent then, and saying
   * 查無此任務 about a perfectly healthy task would be a lie the owner acts on.
   * Defaults true so hand-built fixtures (which pass the whole list) keep
   * working. */
  depsResolvable?: boolean;
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
        ? `${t.office.outsource.label(worker.codename)} · ${worker.model || "—"} · ${
            t.tasks.effortOf[worker.effort] ?? worker.effort
          }`
        : releasedExecutorCodename
          ? t.office.outsource.label(releasedExecutorCodename)
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
        text: cn ? t.office.outsource.label(cn) : task.creatorId,
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
      return { text: cn ? t.office.outsource.label(cn) : id, peerId: id };
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
  // Same pending-flag shape for the 等待外部 step jump (T-c514). Kept as its own
  // flag rather than a shared "scroll to something" one: the two resolve their
  // target differently (reply card id vs step status) and can in principle both
  // be armed on one card, so collapsing them would make the second click cancel
  // the first instead of queueing.
  const [stepScrollPending, setStepScrollPending] = useState(false);
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

  // 等待外部 step jump (T-c514, owner 2026-07-20). Mirrors the reply-card jump
  // above: the click arms a pending flag, and this effect resolves it once the
  // expanded card has actually rendered its timeline (post-hydrate).
  //
  // Why this exists at all: T-c514 removed the task-level reason block, so the
  // waitingReason now lives ONLY inside the step. Reading it went from "glance
  // at the card top" to "expand, then find the right step" — the jump is what
  // pays that cost back.
  //
  // Target = the FIRST waiting_external step. First, not last: steps run in
  // order, so the earliest one still waiting is the one actually blocking. No
  // fallback to "some other step" if none matches — silently scrolling to an
  // unrelated step would be worse than not moving (the reply-card jump's
  // cards[0] fallback is for a DIFFERENT case: the card raced a refetch).
  //
  // THE FLAG MUST SURVIVE THE HYDRATE. The click arms this from a COLLAPSED
  // card, where view.steps is still [] — steps only arrive with the detail
  // fetch. Clearing the flag on "no target yet" would therefore cancel the jump
  // in the very case owner asked for it: by the time the steps land the flag is
  // already false and nothing scrolls. So an empty step list means WAIT (leave
  // the flag armed, the effect re-runs on detail/hydrating/steps), while a
  // loaded list with no waiting step means genuinely nothing to jump to → clear.
  // The reply-card jump above has this property implicitly (it never clears on
  // a miss); spelling it out here because the first version of this effect did
  // clear, and a review caught it — the menu item still opened the card, so the
  // bug looked like a working feature.
  useEffect(() => {
    if (!stepScrollPending) return;
    if (!expanded) {
      setStepScrollPending(false);
      return;
    }
    const target = view.steps.find((s) => s.status === "waiting_external");
    if (!target) {
      // Only give up once we actually HAVE the steps to look through.
      if (view.steps.length > 0) setStepScrollPending(false);
      return;
    }
    // Attribute compare rather than a `[data-step-id="…"]` selector: step ids
    // are server-minted, and building a selector out of them would need
    // CSS.escape, which does not exist in the jsdom the unit tests run in (it
    // would throw there while working in a browser — an untestable branch).
    const el = Array.from(
      rootRef.current?.querySelectorAll('[data-testid="task-step"]') ?? []
    ).find((n) => n.getAttribute("data-step-id") === target.id);
    if (el) {
      el.scrollIntoView({ behavior: "smooth", block: "center" });
      setStepScrollPending(false);
    }
  }, [stepScrollPending, expanded, detail, hydrating, view.steps]);

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
        /* T-c514: the 等待外部 menu jump needs to land on a SPECIFIC step, and
           data-testid alone can't tell two steps apart. Same addressing trick
           the reply-card jump already uses (data-reply-card-id). */
        data-step-id={step.id}
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
        {/* 等待外部 reason (T-9ca5): the step's own one-line reason. Since
            T-c514 this is the ONLY place the reason is rendered — the task-card
            copy it used to mirror up to was removed as duplicate. Agent free
            text → <Markdown>; label on its own line (always-stack). */}
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
                  {/* ── JUMPS FIRST (T-c514, owner 2026-07-20) ──
                      Both jumps mean "take me to where this is stuck", and both
                      now sit ABOVE 標記重複/終止. Owner's wording: 「跳到等我回覆
                      卡或是跳到這個等待外部的地方,都應該在第一個選項」.

                      The ordering argument, so nobody "tidies" it back: these
                      are frequent NAVIGATION, while 標記重複/終止 are rare and
                      DESTRUCTIVE. Putting navigation underneath two destructive
                      items made the common case travel past the dangerous ones.
                      (This supersedes only the POSITION of the v5 jump item —
                      owner's 2026-07-17 ruling that the jump is a menu item
                      rather than a one-click badge still stands.)

                      Both are conditional, so on an ordinary card the menu is
                      unchanged and 標記重複 is still the first item. */}
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
                  {/* 等待外部 jump — the gate is a UNION, and both halves earn
                      their place:

                      • task.status === "waiting_external" — the ONLY signal
                        available on a COLLAPSED card. Steps arrive with the
                        detail hydrate, so an unexpanded card has view.steps
                        empty; gating on steps alone hid the item exactly where
                        owner asks for it (the status badge of a card you have
                        not opened yet). Caught by the DOM-order test, not by
                        reading the code.
                      • a step being waiting_external — because the derived task
                        status can be something ELSE while a step waits on a
                        third party. The real case is 等我回覆 winning the
                        precedence: RecomputeTaskStatus (server domain.go) ranks
                        anyWaitingOwner ABOVE anyWaitingExternal, so a task with
                        one gate awaiting the owner AND one step blocked on a
                        vendor reads waiting_owner, and the step-side reason
                        would be unreachable from the menu without this half.
                        (An earlier revision of this comment claimed the case
                        was in_progress + a waiting step; a review checked the
                        server and showed that ordering makes it unreachable.
                        The half stays — the justification was wrong, not the
                        code.)

                      Neither half subsumes the other, so it is an OR.

                      `!closed` gates the whole thing: closeTask does not roll
                      steps back and RecomputeTaskStatus returns early on a
                      terminal task, so a task terminated while waiting keeps a
                      frozen waiting_external step forever. Offering to jump
                      "to what we are waiting for" on a task nobody waits for
                      any more is a lie; it also read inconsistently (absent
                      while collapsed, since the closed task's own status is
                      terminal, then appearing once expanded). */}
                  {!closed &&
                    (task.status === "waiting_external" ||
                      view.steps.some((s) => s.status === "waiting_external")) && (
                    <button
                      type="button"
                      role="menuitem"
                      className="task-card__menu-item"
                      data-testid="task-status-jump-external"
                      onClick={() => {
                        setStatusOpen(false);
                        setExpanded(true);
                        setStepScrollPending(true);
                      }}
                    >
                      {t.tasks.statusJumpExternal}
                    </button>
                  )}
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
      {/* ── 新舊並存,不是打架 (T-c21e, owner 2026-07-20 於 T-1d82 驗收) ──
           Owner: 「還是這些任務可以直接在最前面接 label 顯示這些 task 目前
           的狀態?」 So each dep row now LEADS with a 狀態 badge. Read next to
           the 2026-07-17 note above, that looks like a reversal. It is not,
           and the difference is WHOSE status the badge is about:

           · The old note bans the badge from ROW 1 (the card's own badge row,
             beside 優先權 / 狀態 / 轉派中). Everything standing there is a
             claim about THIS card. A dep badge there would read 「這張卡是
             等待中」, which is false for an in_progress-and-blocked card. That
             ban still holds — nothing below touches row 1.
           · The new badge lives INSIDE the dep row, inboard of 「等 T-xxxx
             <標題>」. Its subject is unambiguous from position alone: it is
             the status OF THE DEP TASK named on the same line, exactly like
             the 編號 and 標題 beside it. It says nothing about this card.

           Same widget, two different sentences, because in HTML position is
           what fixes the subject. The 2026-07-17 concern (a card claiming two
           statuses) is untouched, so both notes stay true and neither needs
           to be softened. If a future ticket wants a dep-derived badge on row
           1, that is the banned shape — re-read the note above first.

           Colour/wording come from the ONE task-level source (task-badge
           --status-* + t.tasks.status[...]), the same pair row 1's chip uses.
           Not lib/stepBadge.ts — that vocabulary is the STEP's, and a dep is
           a task. Nothing new was invented; only the size is trimmed to the
           row's 12.5px scale (.task-card__dep-status in tasks.css). */}
      {/* T-1d82 (owner 2026-07-20): the row used to be a dead <div> printing
           `等 <task_no>` — and for a dep that had already closed, not even that:
           the lookup missed and it fell back to the raw id (`等 t-35e06c8e63c8`,
           what owner screenshotted). Two fixes, one cause — TasksPage now loads
           the closed population whenever any card carries a dep, so `dep`
           resolves for terminal deps too (see TasksPage's needClosed).
           Here the row gains what the id never carried: the dep's TITLE (that is
           what "看不出在等什麼" was asking for) and a click through to that card,
           reusing the duplicated-link vocabulary right below (navigateHash +
           ExternalLinkIcon) rather than inventing a second way to jump.
           Terminal deps stay IN the list but recede — CheckIcon + dimmed — so a
           long-blocked card keeps its history without spending attention on it
           (Kyle's default; owner may retune at 驗收).
           A dep whose task cannot be resolved AT ALL (deleted / bad id) is the
           one shape that must not be clickable: the anchor would land on an
           empty filter. It says so in words instead. */}
      {task.deps.length > 0 && (
        <div className="task-card__deps">
          {task.deps.map((depId) => {
            const dep = allTasks.find((x) => x.id === depId);
            if (!dep) {
              // Two different silences, and only one of them is 查無此任務.
              // While the list is still the open-only fast path, a dep that
              // simply CLOSED is missing from it too — claiming the task does
              // not exist during those frames would be a confident lie about a
              // healthy task (worse than the raw id it replaced). So until the
              // full population is in hand, the row says only what is true:
              // it is waiting on this id, and cannot name it yet.
              const unknown = !depsResolvable;
              return (
                <div
                  key={depId}
                  className={`task-card__waiting task-card__waiting--dep${
                    unknown ? "" : " task-card__waiting--dep-missing"
                  }`}
                  data-testid="task-dep"
                  data-dep-state={unknown ? "unresolved" : "missing"}
                  // The FULL id, kept within reach ON DESKTOP (T-c21e).
                  // Shortening the visible number is what owner asked for, but
                  // the short form is display-only and can collide — for an
                  // UNRESOLVABLE dep it is the only handle anyone has, and it
                  // is now a lossy one. `title` gives the exact id back
                  // without spending a pixel of the row.
                  // 🔴 It is NOT a complete answer, and the gap runs the wrong
                  // way: `title` needs a hover, and owner reads the cockpit
                  // mostly ON A PHONE, where there is none. So on the surface
                  // that matters most, an unresolvable dep is the short number
                  // and nothing else. Recorded here rather than smoothed over
                  // — if that loss ever bites, this is the line to revisit
                  // (a tap-to-reveal or an inline id would be the fix, both of
                  // which cost row width owner did not agree to spend).
                  // Safe here in a way it would not be on the resolved
                  // <button> below: a div has no accessible name to displace.
                  title={depId}
                >
                  {/* 🔴 NO status badge on this branch, on purpose (T-c21e).
                      There is no `dep`, so there is no status to show — and a
                      badge is the most confident-looking widget on the row.
                      Defaulting it to 尚未執行 would invent a fact; an empty
                      pill would look like a real, momentarily-blank status.
                      Both launder "we don't know" into "we know". That is the
                      same honesty line the unresolved/missing split above was
                      drawn for, one element further in.
                      Silence over a 「狀態不明」 chip: the row's own words
                      (「等 <id>」 vs 「查無此任務」) already say which of the
                      two silences this is, so a chip would be a third way to
                      say what is written right beside it — and it would spend
                      width on the row this ticket just made tighter (see the
                      390px guard). Absence of a badge on a row that otherwise
                      always has one IS the signal. */}
                  <ClockIcon size={13} />
                  {/* The NUMBER is derived, not the raw id (T-c21e, owner
                      2026-07-20: 「那些 ID 應該要跟任務卡上面顯示的一樣,任務
                      卡上的 ID 似乎沒這麼長」). Both of these branches used to
                      print `depId` whole — `t-1d8292a2f8db` where every other
                      surface says `T-1d82` — because with no `dep` in hand
                      there was no server-supplied task_no to print. There
                      never needed to be one: task_no is a pure projection of
                      the id (deriveTaskNo mirrors the server's), so the short
                      form is computable right here.
                      Resolved rows below still print `dep.taskNo` — same
                      value, but from the source. Deriving is the fallback,
                      not the default: if the server ever changes the
                      projection, the surface that matters most keeps agreeing
                      with it for free. */}
                  <span>
                    {unknown
                      ? t.tasks.blockedBy(deriveTaskNo(depId))
                      : t.tasks.blockedByMissing(deriveTaskNo(depId))}
                  </span>
                </div>
              );
            }
            const depClosed = TERMINAL.has(dep.status);
            return (
              <button
                key={depId}
                type="button"
                className={`task-card__waiting task-card__waiting--dep${
                  depClosed ? " task-card__waiting--dep-closed" : ""
                }`}
                data-testid="task-dep"
                data-dep-state={depClosed ? "closed" : "open"}
                // 🔴 NOT aria-label. An aria-label WINS over the button's own
                // text in accname computation, so labelling this 「跳到 T-35e0」
                // would delete the dep's TITLE from the accessibility tree —
                // the one thing this ticket exists to add, removed for exactly
                // the users who cannot see the truncated row. `title` (the
                // duplicated-link's convention right below) describes the
                // action WITHOUT displacing the name, and leaves the full
                // title readable to a screen reader even though CSS truncates
                // it visually.
                title={t.tasks.depJump(dep.taskNo)}
                onClick={() => navigateHash({ page: "tasks", taskId: depId })}
              >
                {/* 最前面的 label = THIS DEP's status (T-c21e, owner
                    2026-07-20). First child, ahead of the ⏱/✓, because owner
                    asked for it 「在最前面」 and because a reader scanning a
                    stack of dep rows wants the verdict before the name.
                    It is plain text in a <span>, so it joins the button's
                    accessible name ahead of 編號 + 標題 rather than replacing
                    them — the aria-label trap noted below is exactly the
                    thing a badge must not re-open. */}
                <span
                  className={`task-badge task-badge--status-${dep.status} task-card__dep-status`}
                  data-testid="task-dep-status"
                >
                  {t.tasks.status[dep.status] ?? dep.status}
                </span>
                {depClosed ? <CheckIcon size={13} /> : <ClockIcon size={13} />}
                <span className="task-card__dep-no">
                  {t.tasks.blockedBy(dep.taskNo)}
                </span>
                {/* Full text on hover: the row truncates at one line, and the
                    title attribute is the only affordance that survives both
                    the truncation and a keyboard-less read of the card. */}
                <span className="task-card__dep-title" title={dep.title}>
                  {dep.title}
                </span>
                <ExternalLinkIcon size={11} />
              </button>
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

      {/* The task-level 等待中 + waitingReason block that used to sit here
          (T-a20b) is GONE (T-c514, owner 2026-07-20). waiting_external is
          reported per-STEP, and the step renders its own reason inside the
          node (see renderStep → .task-step__waiting) — so this block was a
          second copy of the same sentence, one level further from the work it
          describes. What survives at the task level is the 狀態 badge pill
          ("等待外部", row 1): the STATUS still belongs on the card, only the
          duplicated REASON text left. Verified before removing that the step
          block really renders (step-waiting-external.ct.spec.tsx) so the
          reason is never homeless. ── */}

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
              {/* Same rule as the dep rows above (T-c21e): prefer the
                  server's task_no, DERIVE the short number when the original
                  is not in the loaded population — never print the raw id.
                  This row was the third mouth of the same regression; the
                  ticket named only the two dep branches, but a surface owner
                  reads with the same eyes should not disagree with them. */}
              {t.tasks.duplicateOf(
                allTasks.find((x) => x.id === task.duplicateOf)?.taskNo ??
                  deriveTaskNo(task.duplicateOf)
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
                  ? t.office.outsource.label(worker.codename)
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
                  : worker
                    ? t.office.outsource.label(worker.codename)
                    : t.tasks.outsource
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
