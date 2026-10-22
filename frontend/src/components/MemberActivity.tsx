import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import type { MemberActivity, MemberLifecycle } from "../types";
import { buildActivityTree, type ActivityTreeNode } from "../lib/activityTree";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { InstantHint } from "./InstantHint";
import { presenceVisual } from "./LifecycleDot";
import { CoffeeIcon, CloseIcon, HourglassIcon, MoonIcon, SparkleIcon, UnknownIcon } from "./icons";
import "./member-detail.css";
import "./confirm-modal.css";
import "./member-activity.css";

/** Under a minute the text counts seconds, so it has to move every second;
 * past that it counts minutes and a half-minute tick is enough. */
function tickMs(activity: MemberActivity, nowSec: number): number {
  const starts = [
    ...(activity.busySince !== null ? [activity.busySince] : []),
    ...activity.subagents.map((s) => s.startedTs),
  ];
  return starts.some((s) => nowSec - s < 60) ? 1_000 : 30_000;
}

function useActivityNow(activity: MemberActivity | null | undefined): number {
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    if (!activity) return;
    const id = setTimeout(() => setNow(Date.now() / 1000), tickMs(activity, now));
    return () => clearTimeout(id);
  }, [activity, now]);
  return activity ? Date.now() / 1000 : now;
}

/** The 工作中 glyph: a twinkling star (owner's ruling). Every working icon goes
 * through this one component, so a swap is one edit. */
export function ActivityWorkingIcon({ size = 14 }: { size?: number }) {
  return (
    <span className="activity-icon__glyph activity-icon__glyph--working">
      <SparkleIcon size={size} />
    </span>
  );
}

export function ActivityStateIcon({
  state,
  size = 14,
}: {
  state: MemberActivity["state"];
  size?: number;
}) {
  if (state === "busy") return <ActivityWorkingIcon size={size} />;
  return (
    <span className={`activity-icon__glyph activity-icon__glyph--${state}`}>
      {state === "waiting" ? <HourglassIcon size={size} /> : <CoffeeIcon size={size} />}
    </span>
  );
}

/** A member's name led by its status icon. List rows, chat header and detail
 * identity card all render this, so the icon's place is decided here alone.
 * Online and stopping show the reported state, or the unknown mark before a
 * report; waking always shows the unknown mark; anything else gets the moon. `layout="list"`: the icon sits on the
 * first line of a wrapped name. `layout="group"`: icon and name form one box,
 * for a line that wraps its other items onto the next line. */
export function NameWithActivity({
  activity,
  presence,
  memberName,
  testId,
  layout,
  children,
}: {
  activity: MemberActivity | null | undefined;
  presence: MemberLifecycle | undefined;
  memberName?: string;
  testId?: string;
  layout?: "list" | "group";
  children: ReactNode;
}) {
  const icon = (
    <ActivityStatusIcon
      activity={activity}
      presence={presence}
      memberName={memberName}
      testId={testId}
      list={layout === "list"}
    />
  );
  if (layout === "group") {
    return (
      <span className="activity-name">
        {icon}
        {children}
      </span>
    );
  }
  return (
    <>
      {icon}
      {children}
    </>
  );
}

/** Given `memberName`, the icon is a button opening the 運作狀況 dialog. */
function ActivityStatusIcon({
  activity: reported,
  presence,
  memberName,
  testId,
  list,
}: {
  activity: MemberActivity | null | undefined;
  presence: MemberLifecycle | undefined;
  memberName?: string;
  testId?: string;
  list: boolean;
}) {
  const { t, msg } = useI18n();
  // Dropping a stale activity when the session stops reporting is what closes
  // an open dialog then; kept, the dialog would reopen and take focus on return.
  // Waking drops it too: the server clears the store only on report_waking, so
  // what it holds then may be the previous session's.
  const reporting = presence === "online" || presence === "stopping";
  const activity = reporting ? reported : null;
  const now = useActivityNow(activity);
  const triggerRef = useRef<HTMLElement | null>(null);
  const [openFor, setOpenFor] = useState<string | null>(null);
  const open = activity != null && openFor !== null && openFor === memberName;
  // Dropped rather than kept hidden: a dialog closed by the activity going away
  // must not reopen, and take focus, when the activity comes back.
  if (openFor !== null && !open) setOpenFor(null);
  // Same box as the button it stands in for, so a list keeps one column.
  const box = memberName === undefined ? "" : " activity-icon--box";
  const listClass = list ? " activity-icon--list" : "";
  if (!activity) {
    const kind = reporting || presence === "waking" ? "unknown" : "offline";
    // Out of the tab order and the a11y tree. The focusable corner dot beside it
    // names the presence: for the moon that is the whole story, for the unknown
    // mark the unknown hint is hover or tap only (owner asked for the moon's
    // behaviour).
    const hint =
      kind === "offline"
        ? t.office.presence[presenceVisual(presence)]
        : presence === "waking"
          ? t.activity.unknownWakingHint
          : t.activity.unknownHint;
    return (
      <InstantHint
        hint={hint}
        focusable={false}
        className={`activity-icon activity-icon--${kind}${box}${listClass}`}
        aria-hidden="true"
        data-testid={testId && `${testId}-${kind}`}
      >
        <span className={`activity-icon__glyph activity-icon__glyph--${kind}`}>
          {kind === "unknown" ? <UnknownIcon size={14} /> : <MoonIcon size={14} />}
        </span>
      </InstantHint>
    );
  }
  const word = t.activity.state[activity.state];
  const hint = msg.memberActivitySummary(activity, now);
  if (memberName === undefined) {
    return (
      <InstantHint
        hint={hint}
        className={`activity-icon activity-icon--${activity.state}${listClass}`}
        role="img"
        aria-label={word}
        data-testid={testId}
      >
        <ActivityStateIcon state={activity.state} />
      </InstantHint>
    );
  }
  return (
    <>
      <InstantHint
        hint={hint}
        className={`activity-icon activity-icon--box activity-icon--button activity-icon--${activity.state}${listClass}`}
        aria-label={msg.memberActivityOpenLabel(activity.state)}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-testid={testId}
        triggerRef={triggerRef}
        onActivate={() => setOpenFor(memberName)}
      >
        <ActivityStateIcon state={activity.state} />
      </InstantHint>
      {open && (
        <MemberActivityDialog
          name={memberName}
          activity={activity}
          testId={testId && `${testId}-dialog`}
          onClose={() => {
            setOpenFor(null);
            triggerRef.current?.focus();
          }}
        />
      )}
    </>
  );
}

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

function keepTabInside(e: ReactKeyboardEvent, root: HTMLElement) {
  const focusables = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
  if (focusables.length === 0) return;
  const first = focusables[0];
  const last = focusables[focusables.length - 1];
  const at = document.activeElement;
  const edge = e.shiftKey ? first : last;
  if (at === edge || !root.contains(at)) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
  }
}

/** Centred on a desktop, a bottom sheet on a phone (member-activity.css). */
function MemberActivityDialog({
  name,
  activity,
  testId,
  onClose,
}: {
  name: string;
  activity: MemberActivity;
  testId?: string;
  onClose: () => void;
}) {
  const { t, msg } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  useEscapeLayer(onClose, rootRef);
  useEffect(() => {
    closeRef.current?.focus();
  }, []);
  return createPortal(
    <div
      ref={rootRef}
      className="confirm-modal activity-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      data-testid={testId}
      // React bubbles portal events through the icon's tree, and the icon sits
      // inside rows that open a chat on click or Enter/Space. Escape must get
      // through: the Esc layer listens on window, after this.
      onClick={(e) => {
        e.stopPropagation();
        if (e.target === e.currentTarget) onClose();
      }}
      onKeyDown={(e) => {
        if (e.key !== "Escape") e.stopPropagation();
        if (e.key === "Tab") keepTabInside(e, e.currentTarget);
      }}
    >
      <div className="confirm-modal__box activity-dialog__box">
        <div className="activity-dialog__head">
          <h2 id={titleId} className="activity-dialog__title">
            {msg.memberActivityDialogTitle(name)}
          </h2>
          <button
            ref={closeRef}
            type="button"
            className="activity-dialog__close"
            aria-label={t.activity.close}
            onClick={onClose}
          >
            <CloseIcon size={16} />
          </button>
        </div>
        <MemberActivityBody activity={activity} label={t.activity.currentState} />
      </div>
    </div>,
    document.body,
  );
}

function mainIconState(a: MemberActivity): MemberActivity["state"] {
  if (a.busySince !== null) return "busy";
  return a.subagents.length > 0 || a.waits.length > 0 ? "waiting" : "idle";
}

function SubagentNode({ node, now }: { node: ActivityTreeNode; now: number }) {
  const { msg } = useI18n();
  const s = node.subagent;
  return (
    <li className="mp-activity__node">
      <div className="mp-activity__row">
        <ActivityWorkingIcon />
        <span className="mp-activity__label">
          <span className="mp-activity__text">{s.description || s.name}</span>
          {s.description && <span className="mp-activity__chip">{s.name}</span>}
        </span>
        <span className="mp-activity__aside">{msg.memberActivityDuration(s.startedTs, now)}</span>
      </div>
      {node.children.length > 0 && (
        <ul className="mp-activity__children">
          {node.children.map((c) => (
            <SubagentNode key={c.subagent.id} node={c} now={now} />
          ))}
        </ul>
      )}
    </li>
  );
}

/** The detail panels' 運作狀況 card; nothing when there is no activity. */
export function MemberActivityCard({
  activity,
  testId,
}: {
  activity: MemberActivity | null | undefined;
  testId: string;
}) {
  const { t } = useI18n();
  if (!activity) return null;
  return (
    <div className="mp-card mp-activity" data-testid={testId}>
      <MemberActivityBody activity={activity} label={t.activity.cardTitle} />
    </div>
  );
}

/** Label and state word, the one-sentence summary, then the tree — the card
 * and the dialog both render exactly this. */
function MemberActivityBody({ activity: a, label }: { activity: MemberActivity; label: string }) {
  const { t, msg } = useI18n();
  const now = useActivityNow(a);
  const tree = buildActivityTree(a.subagents);
  const showTree = a.busySince !== null || a.subagents.length > 0 || a.waits.length > 0;
  const mainAside =
    a.busySince !== null
      ? msg.memberActivitySince(a.busySince, now)
      : a.subagents.length > 0
        ? t.activity.mainReplied
        : "";
  return (
    <>
      <div className="mp-activity__head">
        <span className="mp-card__title">{label}</span>
        <span className={`mp-activity__state mp-activity__state--${a.state}`}>
          {t.activity.state[a.state]}
        </span>
      </div>
      <div className="mp-activity__summary">{msg.memberActivitySummary(a, now)}</div>
      {showTree && (
        <ul className="mp-activity__tree">
          <li className="mp-activity__node">
            <div className="mp-activity__row mp-activity__row--main">
              <ActivityStateIcon state={mainIconState(a)} />
              <span className="mp-activity__label">
                <span className="mp-activity__text">{t.activity.mainAgent}</span>
              </span>
              {mainAside && <span className="mp-activity__aside">{mainAside}</span>}
            </div>
            {(tree.length > 0 || a.waits.length > 0) && (
              <ul className="mp-activity__children">
                {tree.map((n) => (
                  <SubagentNode key={n.subagent.id} node={n} now={now} />
                ))}
                {a.waits.map((w, i) => (
                  <li key={i} className="mp-activity__node">
                    <div className="mp-activity__row mp-activity__row--wait">
                      <ActivityStateIcon state="waiting" />
                      <span className="mp-activity__label">
                        <span className="mp-activity__text">
                          {w.description || msg.memberActivityWaitTag(w, now)}
                        </span>
                      </span>
                      {w.description && (
                        <span className="mp-activity__aside">{msg.memberActivityWaitTag(w, now)}</span>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </li>
        </ul>
      )}
    </>
  );
}
