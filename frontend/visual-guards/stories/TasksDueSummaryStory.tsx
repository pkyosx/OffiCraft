// The 任務頁 in the production shell, seeded with enough overdue, due-soon and
// later tasks that the 即將到期 lane starts below the fold of `.tasks`.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import App from "../../src/App";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import { __injectMockTask, __resetMock } from "../../src/api/mock";
import { LS_THEME } from "../../src/lib/themePaint";
import type { BuiltinThemeId } from "../../src/lib/themeBundleCore";
import { mkTask } from "./taskFixtures";

export function TasksDueSummaryStory({
  theme = "office-light",
  overdue = 6,
  dueSoon = 2,
  later = 6,
}: {
  theme?: BuiltinThemeId;
  overdue?: number;
  dueSoon?: number;
  later?: number;
}) {
  window.localStorage.setItem(LS_THEME, theme);
  document.documentElement.setAttribute("data-theme", theme);
  window.location.hash = "#tasks";
  const [seeded, setSeeded] = useState(false);
  useEffect(() => {
    __resetMock();
    const now = Math.floor(Date.now() / 1000);
    const seed = (n: number, prefix: string, dueTs: (i: number) => number) => {
      for (let i = 0; i < n; i += 1) {
        const id = `t-${prefix}${String(i).padStart(10, "0")}`;
        __injectMockTask(
          mkTask({
            id,
            taskNo: id,
            title: `${prefix} 任務 ${i + 1}`,
            createdTs: now - 3600,
            dueTs: dueTs(i),
          })
        );
      }
    };
    seed(overdue, "od", (i) => now - (i + 1) * 3600);
    seed(dueSoon, "ds", (i) => now + (i + 1) * 3600);
    seed(later, "lt", (i) => now + (i + 5) * 86_400);
    setSeeded(true);
  }, [overdue, dueSoon, later]);
  if (!seeded) return null;
  return (
    <I18nProvider>
      <ReplyCardsProvider>
        <App onLogout={() => {}} />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}
