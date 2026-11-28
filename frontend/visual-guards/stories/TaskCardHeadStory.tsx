// CT story: one TaskCard per status inside the production list chain, so the
// head line (狀態 icon · #ID · 優先權 · 轉派中) and the expanded card's
// 類型／產物／建立者 rows are measured at the card's real x-offset. The first
// card carries every optional head item plus a type, artifacts and a distinct
// creator, so it is the widest head and the fullest set of expanded rows.
import { useEffect } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, serveArtifacts, MIRA, NOOP, WORKERS } from "./taskFixtures";

const TYPE_NAMES = new Map([["develop-officraft", "開發 OffiCraft 前端功能"]]);

const STATUSES = [
  "in_progress",
  "not_started",
  "waiting_owner",
  "waiting_external",
  "ready_for_done",
  "done",
  "terminated",
  "duplicated",
];
const CLOSED = new Set(["done", "terminated", "duplicated"]);

const TASKS = STATUSES.map((status, i) =>
  serveArtifacts(
    mkTask({
      id: `t-head5291a${String(i).padStart(3, "0")}`,
      taskNo: `T-${7690 + i}`,
      title: `卡頭 ${status}`,
      status,
      priority: i === 0 ? "frozen" : "high",
      lock: i === 0 ? "reassigning" : undefined,
      typeKey: i === 0 ? "develop-officraft" : "",
      creatorId: i === 0 ? "owner" : "mira",
      closedTs: CLOSED.has(status) ? 2500 : null,
      dueTs: 9_000_000_000,
      artifactCount: i === 0 ? 1 : 0,
      artifacts:
        i === 0
          ? [
              {
                id: "ta-head-link",
                kind: "link",
                url: "https://github.com/x/y/pull/7690",
                name: "PR #7690",
                description: "",
                mime: "text/uri-list",
                createdTs: 0,
                createdBy: "mira",
                filename: "",
                versionCount: 1,
              },
            ]
          : [],
    })
  )
);

function LanguageSwitch({ language }: { language: "zh" | "en" }) {
  const { language: current, setLanguage } = useI18n();
  useEffect(() => {
    if (language !== current) setLanguage(language);
  }, [language, current, setLanguage]);
  return null;
}

export function TaskCardHeadStory({ language = "zh" }: { language?: "zh" | "en" }) {
  return (
    <I18nProvider>
      <LanguageSwitch language={language} />
      <div className="app">
        <main className="app__main">
          <div className="tasks">
            <section className="tasks__section">
              <div className="tasks__list">
                {TASKS.map((task) => (
                  <TaskCard
                    key={task.id}
                    task={task}
                    members={[MIRA]}
                    typeNames={TYPE_NAMES}
                    workers={WORKERS}
                    nowTs={3000}
                    onTerminate={NOOP as never}
                    onMarkDuplicate={NOOP as never}
                    onSetPriority={NOOP as never}
                    onSendMessage={NOOP as never}
                    onReassign={NOOP as never}
                    onHydrate={(async () => task) as never}
                  />
                ))}
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
