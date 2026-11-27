// CT story: the REAL LorePage against the REAL mock adapter, opened on `hash`
// (the directory by default), inside
// `.app > .app__main` as in App.tsx, so the row gets production's side gutters
// and `.lore` is height-bound and is the real scroll container.
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

const ALL_OPEN: string[] = [];

export function LoreRowHeadStory({
  longManual = false,
  hash = "",
  roleCap,
  dirCollapsed = ALL_OPEN,
}: {
  longManual?: boolean;
  hash?: string;
  /** Lower the member cap so the directory has a target with entries it cannot read. */
  roleCap?: number;
  /** The directory groups saved as folded; null is a first visit. Every group
   * open by default, so a spec can reach every card. */
  dirCollapsed?: string[] | null;
}) {
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
      // A body that fits one line at any width, beside the seed's longer ones:
      // no row, short or long, may clip its body.
      await mockApi.editLoreEntry("L-7", { body: "讀一遍再送。" });
      // Enough cards in Mira's section to scroll it on a short viewport.
      for (let i = 1; i <= 8; i++) {
        await mockApi.writeLoreEntry({
          title: `第 ${i} 條補充的原則`,
          body: "交付前先在乾淨的工作目錄重跑一次，確認結果不是靠本機殘留的檔案才過的。".repeat(1 + (i % 3)),
          loreType: "other",
        });
      }
      if (roleCap !== undefined) await mockApi.patchServerSettings({ loreCapCharsRole: roleCap });
      if (dirCollapsed === null) localStorage.removeItem("oc_lore_dir_collapsed");
      else localStorage.setItem("oc_lore_dir_collapsed", JSON.stringify(dirCollapsed));
      history.replaceState(null, "", window.location.pathname + hash);
      setReady(true);
    })();
  }, [longManual, hash, roleCap, dirCollapsed]);
  if (!ready) return null;
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <LorePage />
        </main>
      </div>
    </I18nProvider>
  );
}
