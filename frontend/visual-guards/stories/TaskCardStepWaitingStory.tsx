// CT story (T-9ca5): an expanded TaskCard whose live step is waiting_external
// with a SHORT 3-char reason. The step gets its OWN 等待外部 badge
// (.task-step-badge--waiting-external) and a .task-step__waiting reason row that
// always-stacks (label line 1, reason line 2) — same argument as card-reflow,
// but on the step's own row inside the timeline. jsdom is blind to the layout.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { STEP_WAITING_EXTERNAL, MIRA, NOOP, WORKERS } from "./taskFixtures";

export function TaskCardStepWaitingStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={STEP_WAITING_EXTERNAL}
        allTasks={[STEP_WAITING_EXTERNAL]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => STEP_WAITING_EXTERNAL) as never}
      />
    </I18nProvider>
  );
}
