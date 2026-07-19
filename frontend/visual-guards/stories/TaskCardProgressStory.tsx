// CT story: a self-contained TaskCard in owner's re-planned 2/5 shape, so the
// spec passes NO function props across the mount bridge.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { REPLANNED_2_OF_5, MIRA, NOOP, WORKERS } from "./taskFixtures";

export function TaskCardProgressStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={REPLANNED_2_OF_5}
        allTasks={[REPLANNED_2_OF_5]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => REPLANNED_2_OF_5) as never}
      />
    </I18nProvider>
  );
}
