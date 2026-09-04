// T-76cd — the STACKING story for the artifact popover's preview overlay.
//
// Every earlier guard for this overlay mounted it with no app chrome around it,
// and that is exactly why four rounds of geometry measurement stayed green
// while owner's phone showed the panel tucked under the tab bar: with no
// `.topbar` and no `.nav-tabs` in the tree there is NOTHING for the overlay to
// lose a stacking comparison to. A competitor has to exist before "who paints
// on top" is even a question.
//
// So this story carries the real chain AND the two competitors by class:
//   .app > [ .topbar, .nav-tabs, .app__main > .tasks > … > .task-card ]
// The card sits at its production x-offset for the same reason
// TaskArtifactsOverflowStory does.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, serveArtifacts, MIRA, NOOP, WORKERS } from "./taskFixtures";

const MD = ["# Global Context", "", "AI 工作室・成員 boot context", "", "x".repeat(40)].join("\n");
const DATA_URL = "data:text/markdown;charset=utf-8," + encodeURIComponent(MD);

// `reassignedFrom` and `dedupeKey` are set on purpose: they are what put the
// 前任 and 識別鍵 rows in the tree, and those two are named in owner's
// regression screenshot alongside 負責探員 / 建立者. The default fixture omits
// both, so a card built from it would let the stacking probe walk right past
// half of what was reported.
// Served from the mock store because the panel FETCHES its rows since T-66
// (see serveArtifacts) — without it the popover opens on its failure state and
// there is no panel for the stacking probe to measure.
const TASK = serveArtifacts(
  mkTask({
    id: "t-76cd5291a007",
    taskNo: "t-76cd5291a007",
    title: "stacking",
    reassignedFrom: "m-prev",
    dedupeKey: "t-76cd-stacking",
    // SIX artifacts, not one. The panel's height is its content's, so a
    // single-row panel is a short box that reaches no further down the card than
    // the 建立者 row — and the stacking probe can only speak about what the panel
    // actually covers. owner's regression screenshot has the 送出 button drawn
    // over the panel, so the panel has to be tall enough to reach it. Six rows is
    // what puts the composer inside its rect at 390×780.
    artifactCount: 6,
    artifacts: [1, 2, 3, 4, 5, 6].map((n) => ({
      id: `ta-md${n === 1 ? "" : n}`,
      kind: "file" as const,
      url: DATA_URL,
      label: n === 1 ? "Global Context.md" : `產物-${n}.md`,
      filename: n === 1 ? "Global Context.md" : `產物-${n}.md`,
      mime: "text/markdown",
      isImage: false,
      attachmentId: `att-md${n}`,
      createdTs: 0,
      createdBy: "mira",
    })),
  })
);

export function ArtifactsStackingStory() {
  return (
    <I18nProvider>
      <div className="app">
        <header className="topbar">
          <div className="topbar__brand">OffiCraft</div>
        </header>
        <nav className="nav-tabs">
          <div className="nav-tabs__seg">
            <button type="button" className="nav-tab nav-tab--active">
              <span>辦公室</span>
            </button>
            <button type="button" className="nav-tab">
              <span>任務</span>
            </button>
          </div>
        </nav>
        <main className="app__main">
          <div className="tasks">
            <section className="tasks__section">
              <div className="tasks__list">
                <TaskCard
                  task={TASK}
                  allTasks={[TASK]}
                  members={[MIRA]}
                  workers={WORKERS}
                  nowTs={3000}
                  onTerminate={NOOP as never}
                  onMarkDuplicate={NOOP as never}
                  onSetPriority={NOOP as never}
                  onSendMessage={NOOP as never}
                  onReassign={NOOP as never}
                  onHydrate={(async () => TASK) as never}
                  onRemoveArtifact={NOOP as never}
                />
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
