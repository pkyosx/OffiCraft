// CT story: an expanded task card whose waiting step carries a waiting reply
// card (TaskReplyCard), with options, suggested replies and the answer box,
// against the real mock adapter.
import { useEffect, useState } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { __injectMockReplyCard, __resetMock } from "../../src/api/mock";
import { mkStep, mkTask, MIRA, NOOP, WORKERS } from "./taskFixtures";

const CARD_ID = "rc-5b7152910001";

const TASK = mkTask({
  id: "t-5b7152910002",
  taskNo: "t-5b7152910002",
  title: "Release the settings page",
  status: "waiting_owner",
  progressDone: 1,
  progressTotal: 2,
  steps: [
    mkStep({ status: "done" }),
    mkStep({ status: "waiting_owner", name: "Owner sign-off", replyCardId: CARD_ID }),
  ],
});

function Lang({ language }: { language: "zh" | "en" }) {
  const { language: current, setLanguage } = useI18n();
  useEffect(() => {
    if (language !== current) setLanguage(language);
  }, [language, current, setLanguage]);
  return null;
}

export function TaskCardReplyCardStory({
  language = "zh",
  reassignedFrom = "",
}: {
  language?: "zh" | "en";
  reassignedFrom?: string;
}) {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    __resetMock();
    __injectMockReplyCard({
      id: CARD_ID,
      from: "mira",
      kind: "decision",
      summary: "Ship this version of the settings page now, or wait for the next round?",
      body: "The settings page passes every check; the next round adds the notification options.",
      options: [
        { text: "Ship it now, the notification options can follow", aiPick: true },
        { text: "Wait and ship both together next round", aiPick: false },
      ],
      selectMode: "single",
      status: "waiting",
      attachments: [],
      createdTs: Date.now() / 1000 - 3600,
      answeredTs: null,
      chatMessageId: "msg-1",
      answer: null,
      task: null,
    });
    setReady(true);
  }, []);
  if (!ready) return null;
  return (
    <I18nProvider>
      <Lang language={language} />
      <div className="app">
        <main className="app__main">
          <div className="tasks">
            <TaskCard
              task={{ ...TASK, reassignedFrom }}
              members={[MIRA]}
              workers={WORKERS}
              nowTs={3000}
              onTerminate={NOOP as never}
              onMarkDuplicate={NOOP as never}
              onSetPriority={NOOP as never}
              onSendMessage={NOOP as never}
              onReassign={NOOP as never}
              onHydrate={(async () => ({ ...TASK, reassignedFrom })) as never}
            />
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
