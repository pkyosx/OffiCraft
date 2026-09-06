// T-49fb — the PAGE-OVERFLOW story for the artifact popover.
//
// Why it needs its own story rather than reusing TaskCardArtifactsStory: the
// bug is a coordinate-space error (the popover's width was clamped against
// 100vw while its LEFT edge starts at the card's content edge), so it is only
// observable when the card sits at its production x-offset. A bare card mounted
// at x≈0 has ~22px of slack it does not have in the app and the overflow
// vanishes — that is exactly why the existing 390px guards were green while the
// owner's phone scrolled sideways.
//
// So this story reproduces the REAL ancestor chain by class:
//   .app > .app__main (max-width 1040 + 22px side padding) > .tasks
//   (overflow-y:auto ⇒ overflow-x:auto, the box that actually grew the phantom
//   scrollbar) > .tasks__section > .tasks__list > .task-card
// Those are production classes carrying production padding, so the geometry the
// guard measures is the geometry that ships.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, serveArtifacts, MIRA, NOOP, WORKERS } from "./taskFixtures";

// The owner's actual card: a link artifact whose NAME is a long, barely
// breakable branch name.
//
// ⚠️ 48 runes is the WRITE cap on a STORED name (T-92) and this one is 62, which
// is legitimate because the cap gates writes and says nothing about what a read
// returns. It is NOT the migration that produces such rows: 00086 set a link's
// name to `substr(label, 1, 48)`, so all 339 over-length migrated link labels
// came out CUT to 48. What is genuinely uncapped is the DERIVED name — a row
// with no stored name falls back to the link's target url, which is any length
// at all — and that is the class of row this overflow story stands in for.
// Served from the mock store: since T-66 the popover fetches its rows rather
// than reading them off the card (see serveArtifacts).
const OWNER_LINK = serveArtifacts(
  mkTask({
    id: "t-23cf5291a001",
    taskNo: "t-23cf5291a001",
    title: "drop delegation whitelist",
    artifactCount: 1,
    artifacts: [
      {
        id: "ta-link",
        kind: "link",
        url: "https://github.com/x/officraft/tree/t-23cf-drop-delegation-whitelist",
        name: "branch t-23cf-drop-delegation-whitelist-and-reconcile-the-gate",
        description: "",
        mime: "text/uri-list",
        createdTs: 0,
        createdBy: "mira",
      },
    ],
  })
);

export function TaskArtifactsOverflowStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="tasks">
            <section className="tasks__section">
              <div className="tasks__list">
                <TaskCard
                  task={OWNER_LINK}
                  allTasks={[OWNER_LINK]}
                  members={[MIRA]}
                  workers={WORKERS}
                  nowTs={3000}
                  onTerminate={NOOP as never}
                  onMarkDuplicate={NOOP as never}
                  onSetPriority={NOOP as never}
                  onSendMessage={NOOP as never}
                  onHydrate={(async () => OWNER_LINK) as never}
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
