// CT story (T-9ca5): a reassigned TaskCard — lock="reassigning" ORTHOGONAL to
// its honest derived status (in_progress). The guard proves the 轉派中 LOCK
// overlay badge renders BESIDE the status badge, inside the card at every width
// (jsdom can't see the badge-row geometry; this measures the real layout).
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { LOCKED_REASSIGNING, MIRA, NOOP, WORKERS } from "./taskFixtures";

export function TaskCardLockStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={LOCKED_REASSIGNING}
        allTasks={[LOCKED_REASSIGNING]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => LOCKED_REASSIGNING) as never}
      />
    </I18nProvider>
  );
}
