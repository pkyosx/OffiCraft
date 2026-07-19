// CT story: a waiting_external TaskCard with a short 3-char reason — the
// always-stack fixture (T-a20b). The reason trivially fits beside the 等待中
// label, so its landing on line 2 is caused by the CSS rule, not by overflow.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { WAITING_SHORT, MIRA, NOOP, WORKERS } from "./taskFixtures";

export function TaskCardWaitingStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={WAITING_SHORT}
        allTasks={[WAITING_SHORT]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => WAITING_SHORT) as never}
      />
    </I18nProvider>
  );
}
