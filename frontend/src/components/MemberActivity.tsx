import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { MemberActivity } from "../types";
import { buildActivityTree, type ActivityTreeNode } from "../lib/activityTree";
import { InstantHint } from "./InstantHint";
import { CoffeeIcon, EllipsisIcon, HourglassIcon } from "./icons";
import "./member-detail.css";
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

/** The 工作中 glyph (three dots bouncing in turn, owner rc-89f97eb28af9).
 * Every working icon goes through this one component, so a swap is one edit. */
export function ActivityWorkingIcon({ size = 14 }: { size?: number }) {
  return (
    <span className="activity-icon__glyph activity-icon__glyph--working">
      <EllipsisIcon size={size} />
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
 * identity card all render this one. Nothing when there is no activity. */
export function ActivityStatusIcon({
  activity,
  testId,
}: {
  activity: MemberActivity | null | undefined;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const now = useActivityNow(activity);
  if (!activity) return null;
  const word = t.activity.state[activity.state];
  return (
    <InstantHint
      hint={msg.memberActivitySummary(activity, now)}
      className={`activity-icon activity-icon--${activity.state}`}
      role="img"
      aria-label={word}
      data-testid={testId}
    >
      <ActivityStateIcon state={activity.state} />
    </InstantHint>
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
  const { t, msg } = useI18n();
  const now = useActivityNow(activity);
  if (!activity) return null;
  const a = activity;
  const tree = buildActivityTree(a.subagents);
  const showTree = a.busySince !== null || a.subagents.length > 0 || a.waits.length > 0;
  const mainAside =
    a.busySince !== null
      ? msg.memberActivitySince(a.busySince, now)
      : a.subagents.length > 0
        ? t.activity.mainReplied
        : "";
  return (
    <div className="mp-card mp-activity" data-testid={testId}>
      <div className="mp-activity__head">
        <span className="mp-card__title">{t.activity.cardTitle}</span>
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
                          {w.description || msg.memberActivityWaitTag(w)}
                        </span>
                      </span>
                      {w.description && (
                        <span className="mp-activity__aside">{msg.memberActivityWaitTag(w)}</span>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </li>
        </ul>
      )}
    </div>
  );
}
