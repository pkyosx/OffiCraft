import { useEffect } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, MIRA, NOOP, WORKERS } from "./taskFixtures";

declare global {
  interface Window {
    __dueSaves: (number | null)[];
    __leadSaves: (number | null | undefined)[];
  }
}

const DUE_OCT_30 = Math.floor(new Date(2099, 9, 30, 23, 59, 59).getTime() / 1000);

const TASK = mkTask({
  id: "t-due",
  taskNo: "T-due",
  title: "鍵盤改到期日",
  status: "in_progress",
  dueTs: DUE_OCT_30,
});

function LanguageSwitch({ language }: { language: "zh" | "en" }) {
  const { language: current, setLanguage } = useI18n();
  useEffect(() => {
    if (language !== current) setLanguage(language);
  }, [language, current, setLanguage]);
  return null;
}

export function TaskCardDueStory({
  dueTs = DUE_OCT_30,
  dueSoonLeadMinutes,
  language = "zh",
}: {
  dueTs?: number;
  dueSoonLeadMinutes?: number;
  language?: "zh" | "en";
}) {
  const task =
    dueTs === DUE_OCT_30 && dueSoonLeadMinutes === undefined
      ? TASK
      : { ...TASK, dueTs, dueSoonLeadMinutes };
  window.__dueSaves = [];
  window.__leadSaves = [];
  return (
    <I18nProvider>
      <LanguageSwitch language={language} />
      <div className="app">
        <main className="app__main">
          <div className="tasks">
            <div data-testid="outside" style={{ height: 40 }} />
            <TaskCard
              task={task}
              members={[MIRA]}
              workers={WORKERS}
              nowTs={Date.now() / 1000}
              onTerminate={NOOP as never}
              onMarkDuplicate={NOOP as never}
              onSetPriority={NOOP as never}
              onSetDue={async (_id, dueTs, lead) => {
                window.__dueSaves.push(dueTs);
                window.__leadSaves.push(lead);
              }}
              onSendMessage={NOOP as never}
              onReassign={NOOP as never}
              onHydrate={(async () => task) as never}
            />
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
