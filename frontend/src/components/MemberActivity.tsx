import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import type { MemberActivity } from "../types";
import { buildActivityTree, type ActivityTreeNode } from "../lib/activityTree";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { InstantHint } from "./InstantHint";
import { CoffeeIcon, CloseIcon, HourglassIcon, SparkleIcon } from "./icons";
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

/** The status icon beside a member's name — list rows, chat header and detail
 * identity card all render this one. Nothing when there is no activity.
 * Given `memberName`, the icon is a button opening the 運作狀況 dialog. */
export function ActivityStatusIcon({
  activity,
  memberName,
  testId,
}: {
  activity: MemberActivity | null | undefined;
  memberName?: string;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const now = useActivityNow(activity);
  const [trigger, setTrigger] = useState<HTMLElement | null>(null);
  if (!activity) return null;
  const word = t.activity.state[activity.state];
  const hint = msg.memberActivitySummary(activity, now);
  if (memberName === undefined) {
    return (
      <InstantHint
        hint={hint}
        className={`activity-icon activity-icon--${activity.state}`}
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
        className={`activity-icon activity-icon--button activity-icon--${activity.state}`}
        aria-label={msg.memberActivityOpenLabel(activity.state)}
        aria-haspopup="dialog"
        aria-expanded={trigger !== null}
        data-testid={testId}
        onActivate={setTrigger}
      >
        <ActivityStateIcon state={activity.state} />
      </InstantHint>
      {trigger && (
        <MemberActivityDialog
          name={memberName}
          activity={activity}
          testId={testId && `${testId}-dialog`}
          onClose={() => {
            setTrigger(null);
            trigger.focus();
          }}
        />
      )}
    </>
  );
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
