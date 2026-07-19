// CT stories for the T-3dc5 artifact set: a TaskCard carrying a 3-artifact set
// (WITH), and one with none (NO — the empty-set case). Self-contained so the
// spec passes NO function props across the mount bridge; onHydrate resolves the
// same fixture so the popover fills in a real browser.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { WITH_ARTIFACTS, NO_ARTIFACTS, MIRA, NOOP, WORKERS } from "./taskFixtures";

function Card({ task }: { task: typeof WITH_ARTIFACTS }) {
  return (
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => task) as never}
        onRemoveArtifact={NOOP as never}
      />
    </I18nProvider>
  );
}

export function TaskCardArtifactsStory() {
  return <Card task={WITH_ARTIFACTS} />;
}

export function TaskCardNoArtifactsStory() {
  return <Card task={NO_ARTIFACTS} />;
}
