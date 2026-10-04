// CT story: the REAL LorePage against the REAL mock adapter, inside
// `.app__main` so the row gets production's side gutters.
//
// Every manual-scoped mock entry is keyed to the same manual (tm-mock), so a
// normal name and a too-long name cannot both be on screen at once; `longManual`
// picks which one that manual carries.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { LorePage } from "../../src/components/LorePage";

const NORMAL_MANUAL_NAME = "部署前檢查";
const LONG_MANUAL_NAME =
  "跨租戶出貨追蹤資料對帳與異常通報流程（海運、空運、陸運三段承運商回傳格式差異整理與補件）" +
  "以及月底結算前的應收應付逐筆核對、匯率差額說明與客戶端對帳單重新產出";

export function LoreRowHeadStory({ longManual = false }: { longManual?: boolean }) {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    // Dynamic import: a static one makes Playwright's node-side transform parse
    // mock.ts's `?raw` imports as JavaScript and the spec fails to collect.
    void (async () => {
      const { mockApi, __resetMock, __injectMockTaskType } = await import(
        "../../src/api/mock"
      );
      __resetMock();
      __injectMockTaskType({
        typeKey: "tm-mock",
        displayName: longManual ? LONG_MANUAL_NAME : NORMAL_MANUAL_NAME,
        purpose: "",
      });
      // The owner's screenshot: type → id → 置頂 → ⚙ 任務 chip.
      await mockApi.setLoreEntryState("L-4", "pinned");
      setReady(true);
    })();
  }, [longManual]);
  if (!ready) return null;
  return (
    <I18nProvider>
      <div className="app__main">
        <LorePage />
      </div>
    </I18nProvider>
  );
}
