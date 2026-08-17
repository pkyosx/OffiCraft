// CT story for 設定 › 角色誌 › 啟動程序 — the INDEX of two documents (T-bac4;
// it was the page that stacked both of them until the owner replaced that
// shape).
//
// It renders the REAL page, not a hand-built pair of rows: the claim under
// measurement is "both documents are reachable on one phone screen", and a
// story that assembled its own rows would answer for itself rather than for
// what SettingsPage renders. The route is walked the way a person walks it
// (角色誌 → 啟動程序) so the ancestor chain and the breadcrumbs are the app's own.
//
// The documents are seeded THROUGH THE REAL ADAPTER at the length that used to
// reproduce the defect (the first document is thousands of pixels tall).
// ⚠️ That length is NOT load-bearing any more, and the guard's header says so
// with the measurement: on an index that renders no document, cutting the
// fixture to one section leaves every test green. It is kept because it keeps
// the fixture resembling the real page, not because anything depends on it.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { SettingsPage } from "../../src/components/SettingsPage";
import { api } from "../../src/api";

/** Long enough that the first document alone overflows a phone screen many
 * times over — the real 啟動程序 documents are ~1,800 characters of CJK prose
 * with headings, which lays out to several thousand pixels at 390px wide. */
const doc = (runtime: string) =>
  [
    `# 啟動程序（${runtime} · 版面守衛用）`,
    "",
    ...Array.from({ length: 40 }, (_, i) => [
      `## ${i + 1}. 這一節存在的目的是把這份文件撐得比手機螢幕高很多`,
      "",
      "剛醒過來、開機當下依序做這四步：先報 waking，接著把脈絡接回來，全部就緒之後才掛上事件流，最後盤點手上還沒結束的任務並排程。",
      "",
    ]).flat(),
  ].join("\n");

export function BootNavStory() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let alive = true;
    void Promise.all([
      api.saveBootDoc("boot_sequence", "claude", doc("Claude Code")),
      api.saveBootDoc("boot_sequence", "codex", doc("Codex CLI")),
    ]).then(() => alive && setReady(true));
    return () => {
      alive = false;
    };
  }, []);

  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">{ready && <SettingsPage />}</main>
      </div>
    </I18nProvider>
  );
}
