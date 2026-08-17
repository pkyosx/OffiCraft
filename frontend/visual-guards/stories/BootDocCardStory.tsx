// CT story for the boot-context block page at phone widths.
//
// Renamed from BootDocSectionRowStory (T-c33e): the page no longer has section
// rows. What it still has — and what the guard beside it still measures — is
// the 還原出廠版 recovery button and a card whose content is owner/agent prose:
// long CJK headings normally, and, since these documents name routes, ids and
// file paths, sometimes a long unbreakable token. That token used to sit in a
// section LABEL (`.boot-doc-sec__label`); it now sits in the rendered document
// (`.doc-md`), which is where the same defect class lives for every other
// document surface in the cockpit (frontend/CLAUDE.md 〈長 token 溢出〉).
//
// jsdom cannot see any of this: it applies no layout engine, so the buttons are
// in the DOM whether or not they are on screen.
//
// The ancestor chain is reproduced BY CLASS, per frontend/CLAUDE.md 〈浮層寬度
// 不可用 vw 夾〉: a bare card mounted at x≈0 carries ~22px of slack it does not
// have in the app, which is how earlier 390px guards stayed green while the
// owner's phone was broken. Production is
//   .app > .app__main (max-width 1040 + 22px side padding) > .settings > card.
//
// The document is seeded THROUGH THE REAL ADAPTER (`api.saveBootDoc`) rather
// than passed in as a prop: BootDocPage has no text prop — it reads the live
// document — and a story that faked one would be measuring a page that does not
// exist. The page is mounted only once the write has landed, so the guard never
// races the first paint.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { BootDocPage } from "../../src/components/BootDocPage";
import { api } from "../../src/api";
import { zh } from "../../src/i18n/locales/zh";

/** Long CJK headings (what these documents really look like) plus one line
 * carrying an unbreakable token of the kind the boot docs actually cite. */
const DOC = [
  "# 啟動程序（Boot Sequence · 版面守衛用）",
  "",
  "## 1. 世界觀 — 這個世界怎麼合作（你是誰、你活在哪、誰聽得到你說的話）",
  "",
  "先把自己接回來，再決定要做什麼。",
  "",
  "## 2. 端點 /api/document-history/boot_sequence/claude/restore?verbose=true&trace=abcdef0123456789",
  "",
  "這一段的標題帶著一個斷不開的長 token。",
  "",
  "3. **報 waking（不掛 SSE）。** 用 MCP `report_waking()` 回報你已經開機，並且在確認就緒之後才掛上 listener。",
  "",
].join("\n");

export function BootDocCardStory() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let alive = true;
    void api
      .saveBootDoc("boot_sequence", "claude", DOC)
      .then(() => alive && setReady(true));
    return () => {
      alive = false;
    };
  }, []);

  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          {ready && (
            <BootDocPage
              kind="boot_sequence"
              docKey="claude"
              title={zh.settings.bootClaudeName}
              historyTitle={zh.settings.historyBootClaudeTitle}
              crumbs={[{ label: zh.settings.title }]}
            />
          )}
        </main>
      </div>
    </I18nProvider>
  );
}
