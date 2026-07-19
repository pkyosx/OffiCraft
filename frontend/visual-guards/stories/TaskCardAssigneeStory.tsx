// CT story: a live TaskCard whose 負責人 display name is long enough to ellipse
// at 390px. The 轉派 icon (owner 2026-07-18) shares the 負責人 value cell — this
// story is the layout the reflow guard measures: the name must shrink/ellipse
// so the icon stays on the row and fully inside the card, never clipped.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { LONG_ASSIGNEE, LONG_MEMBER, NOOP, WORKERS } from "./taskFixtures";

export function TaskCardAssigneeStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={LONG_ASSIGNEE}
        allTasks={[LONG_ASSIGNEE]}
        members={[LONG_MEMBER]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => LONG_ASSIGNEE) as never}
      />
    </I18nProvider>
  );
}
