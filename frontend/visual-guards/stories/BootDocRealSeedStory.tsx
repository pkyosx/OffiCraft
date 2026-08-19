// CT story — the boot-context block page rendering THE REAL
// `seeds/system_interaction.md`, at phone widths.
//
// Why a second boot-doc story: `BootDocCardStory` seeds a hand-written eleven
// line document. The system-interaction seed is the full document and carries
// the things a synthetic fixture does not — fenced blocks, tables, long CJK
// headings, cited routes and ids. "It works on a short document" says nothing
// about it.
//
// T-c33e: the page now renders the whole document as ONE markdown block rather
// than seventy-odd section rows, so the expectations published below are drawn
// from the source rather than from a splitter. The guard checks the whole
// document, not a short prefix.
//
// Nothing is seeded here on purpose. `api/mock.ts` already SERVES the real
// seed for (system_interaction, global) — the same `?raw` bytes `api/seeds.ts`
// imports from the repo-root file — so writing a copy in would replace the
// input with a fake of itself and mark the document owner-edited besides.
// The last heading is computed from those same bytes rather than written down,
// so a seed edit moves the guard's expectation with it.
//
// The ancestor chain is reproduced BY CLASS, per frontend/CLAUDE.md 〈浮層寬度
// 不可用 vw 夾〉: a bare card mounted at x≈0 carries ~22px of slack it does not
// have in the app. Production is
//   .app > .app__main (max-width 1040 + 22px side padding) > .settings > card.
import { I18nProvider } from "../../src/i18n";
import { BootDocPage } from "../../src/components/BootDocPage";
import { SEED_SYSTEM_INTERACTION_MD } from "../../src/api/seeds";
import { zh } from "../../src/i18n/locales/zh";

/** Derive the last heading from the same bytes the mock serves, so the spec
 * proves the whole document rendered rather than a prefix. Fenced blocks are
 * skipped because these seeds contain shell snippets whose lines start with
 * `#`. */
const SOURCE_HEADINGS = (() => {
  let fenced = false;
  const found: string[] = [];
  for (const line of SEED_SYSTEM_INTERACTION_MD.trim().split("\n")) {
    if (/^(?:```|~~~)/.test(line)) {
      fenced = !fenced;
      continue;
    }
    if (!fenced && /^#{1,6} +\S/.test(line)) found.push(line.replace(/^#+ +/, "").trim());
  }
  return found;
})();

export const LAST_HEADING = SOURCE_HEADINGS[SOURCE_HEADINGS.length - 1] ?? "";

export function BootDocRealSeedStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div data-testid="story-last-heading">{LAST_HEADING}</div>
          <BootDocPage
            kind="system_interaction"
            docKey="global"
            title={zh.settings.systemName}
            historyTitle={zh.settings.historyBootSystemTitle}
            crumbs={[{ label: zh.settings.title }]}
          />
        </main>
      </div>
    </I18nProvider>
  );
}
