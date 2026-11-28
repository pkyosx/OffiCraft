// CT story: the REAL 檔案庫 list against the REAL mock adapter, inside
// `.app > .app__main` as in App.tsx, with one file and one link whose names and
// descriptions are too long for a phone line.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { SharedDocsPage } from "../../src/components/SharedDocsPage";

export function SharedDocsListStory() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    // Dynamic import: a static one makes Playwright's node-side transform parse
    // mock.ts's `?raw` imports as JavaScript and the spec fails to collect.
    void (async () => {
      const { mockApi, __resetMock } = await import("../../src/api/mock");
      __resetMock();
      await mockApi.createSharedDoc({
        kind: "link",
        name: "跨租戶出貨追蹤資料對帳與異常通報流程（海運、空運、陸運三段承運商回傳格式差異整理）",
        description: "月底結算前的應收應付逐筆核對、匯率差額說明與客戶端對帳單重新產出的步驟",
        url: "https://example.com/very/long/path/to/the/reconciliation/handbook",
      });
      await mockApi.replaceSharedDoc("sd-3f9a2c71b0e4", {
        name: "PR 審查規則與退件標準：前後端、資料庫遷移與 wire 變更逐項核對清單（2026 下半年版）",
        description: "審查時逐項核對；退件要寫明是哪一條、怎麼改，並附上重現步驟與預期結果",
      });
      setReady(true);
    })();
  }, []);
  if (!ready) return null;
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <SharedDocsPage />
        </main>
      </div>
    </I18nProvider>
  );
}
