// CT story (T-5896) — the monitor §3 AI-session row's STUCK badge.
//
// Mounts the REAL <SessionRow> (not a hand-mirrored skeleton) inside the same
// .mon-table > tbody chain MonitorPage renders, so the badge's presence is
// driven by the actual `session.stuck === true` guard in production code. The
// story takes the stuck flag so one mount covers the true case and another the
// false/undefined case — the guard is boolean, width-independent.
import "../../src/components/monitor.css";
import { I18nProvider } from "../../src/i18n";
import { SessionRow } from "../../src/components/MonitorPage";
import type { MonSessionView } from "../../src/types";

function mkSession(over: Partial<MonSessionView>): MonSessionView {
  return {
    id: "m-1",
    name: "Mira",
    role: "assistant",
    model: "Opus 4.8",
    effort: "high",
    machine: "eva-m5",
    account: "acct-a",
    status: "online",
    contextPct: 42,
    cost: 1.2,
    bankedCost: 0,
    idleSecs: 1200,
    ...over,
  };
}

export function MonitorStuckBadgeStory({ stuck }: { stuck?: boolean }) {
  const session = mkSession({ stuck });
  return (
    <I18nProvider>
      <div className="mon-page">
        <div className="mon-table-wrap" data-surface="sessions">
          <table className="mon-table">
            <tbody>
              <SessionRow
                session={session}
                members={[]}
                dash="—"
                onOpen={() => {}}
              />
            </tbody>
          </table>
        </div>
      </div>
    </I18nProvider>
  );
}
