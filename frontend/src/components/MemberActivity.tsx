import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { MemberActivity } from "../types";
import { InstantHint } from "./InstantHint";
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

/** The chat header's status word; renders nothing when there is no activity. */
export function MemberActivityWord({
  activity,
}: {
  activity: MemberActivity | null | undefined;
}) {
  const { t, msg } = useI18n();
  const now = useActivityNow(activity);
  if (!activity) return null;
  return (
    <>
      <span className="activity-word__sep" aria-hidden="true">
        ·
      </span>
      <InstantHint
        hint={msg.memberActivitySummary(activity, now)}
        className={`activity-word activity-word--${activity.state}`}
        data-testid="chat-header-activity"
      >
        {t.activity.state[activity.state]}
      </InstantHint>
    </>
  );
}

/** The activity block at the top of the detail panels' 運行狀況 card;
 * renders nothing when there is no activity. */
export function MemberActivityDetails({
  activity,
  testId,
}: {
  activity: MemberActivity | null | undefined;
  testId: string;
}) {
  const { msg } = useI18n();
  const now = useActivityNow(activity);
  if (!activity) return null;
  return (
    <div className="mp-activity" data-testid={testId}>
      <div className="mp-activity__summary">{msg.memberActivitySummary(activity, now)}</div>
      {msg.memberActivityDetails(activity, now).map((line, i) => (
        <div key={i} className="mp-activity__line">
          {line}
        </div>
      ))}
    </div>
  );
}
