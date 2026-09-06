// Style ownership: a component that USES a stylesheet's classes must IMPORT it.
//
// The bug this exists for (T-7526): `machine-picker.css` was imported by exactly
// one module, itself reachable from production only through a chain of OTHER
// components' imports. BOTH detail panels render their settings dialog with the
// `.machine-picker*` classes but neither imported the stylesheet — they were
// free-riding on that transitive import. The moment one link in the chain
// stopped being driven, the last production importer went with it and BOTH
// dialogs rendered completely unstyled: no centred box, no backdrop, a raw
// browser <select>. The MEMBER panel — untouched by that change — broke too.
// (Those chain modules have since been deleted; both panels now import the sheet
// directly, which is exactly the state this guard holds them in.)
//
// Nothing caught it. jsdom evaluates no CSS, so the whole vitest suite stayed
// green; `tsc` sees no link between a class string and a stylesheet; and the one
// CT guard that had ever rendered a machine picker was retired in the same
// ticket. It was found by looking at a screenshot.
//
// T-f014 is the same shape of change and so registers its sheet here:
// `.md-preview*` used to live in the middle of office.css, which the overlay
// itself never imported — it was reachable only because OfficePage / RepliesPage
// / TasksPage happen to import the chat sheet. The overlay is mounted from the
// artifact popover and the task card too, and it is now the cockpit's ONLY
// full-size image surface, so the styles moved to md-preview.css and the
// component that draws them owns the import.
//
// So the invariant is checked at the SOURCE level, where it is cheap and exact:
// use a stylesheet's block, own the import. Transitive style inheritance is
// exactly the thing that broke.

import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const DIR = join(__dirname);

/** Stylesheets whose classes are namespaced by a BEM block and are owned by
 * whoever renders them. (Global/shared sheets — chrome, theme, the `.doc-md`
 * document skin — are not component-owned and are deliberately out of scope.)
 *
 * ⚠️ This list is hand-maintained, which is itself the weakness it guards
 * against: forget to add a sheet and the suite stays green while that sheet
 * free-rides again. Two branches each appended to it independently and collided
 * here on merge; the resolution is the UNION, never a pick of one side. Deriving
 * the list from the filesystem (every component `.css` minus a justified
 * exclusion list) is the real fix and is tracked separately. */
const OWNED_SHEETS = [
  "machine-picker.css",
  "member-detail.css",
  "md-preview.css",
  // T-1f39: the retained-revision reader. Mounted from DocumentHistoryEntry —
  // which imports settings.css — so it would free-ride exactly like the two
  // above until the day something else opens it.
  "doc-hist-modal.css",
  // T-33: the 傳承 sheet is drawn by FOUR components (page shell, the subjects
  // view, one entry card, the 尚無資料來源 block) and only the page shell is
  // reachable from App — the other three would free-ride on its import.
  "lore.css",
  // T-60: the artifact version reader. Mounted from TaskArtifactsPopover, which
  // draws the `.task-artifacts*` block out of tasks.css — so this sheet would
  // free-ride on whatever the task page happens to import.
  "task-artifact-versions.css",
] as const;

/** Sheets whose BEM block is not just the filename. `member-detail.css` owns the
 * `.mp-*` block, so deriving the block from the filename made its entry check an
 * EMPTY set — a vacuous green sitting inside the very guard written to catch
 * vacuous greens. It only surfaced when the two branches' versions were merged
 * and the stronger side's non-empty-corpus assertion applied to it.
 *
 * ⚠️ The value is a block PREFIX, not one exact block: member-detail.css holds a
 * family (`mp-identity__*`, `mp-card__*`, `mp-confirm__*`, …). Matching only the
 * literal `mp__` found exactly one file and left the four real consumers
 * unchecked — non-empty, and still nearly vacuous. Corpus size is not the same
 * question as corpus coverage. */
const BLOCK_OVERRIDES: Record<string, string> = {
  "member-detail.css": "mp",
  "task-artifact-versions.css": "ta-versions",
};

function blockOf(sheet: string): string {
  return BLOCK_OVERRIDES[sheet] ?? sheet.replace(/\.css$/, "");
}

function componentsUsing(block: string): string[] {
  const users: string[] = [];
  for (const file of readdirSync(DIR)) {
    if (!file.endsWith(".tsx") || file.endsWith(".test.tsx")) continue;
    const src = readFileSync(join(DIR, file), "utf8");
    // Only count REAL usage in markup, not a mention in prose/comments.
    if (new RegExp(`className=[{"\`][^"\`}]*\\b${block}[a-z0-9-]*__`).test(src)) users.push(file);
  }
  return users;
}

describe("component style ownership", () => {
  for (const sheet of OWNED_SHEETS) {
    const block = blockOf(sheet);

    it(`every component using .${block}__* imports ./${sheet}`, () => {
      const users = componentsUsing(block);
      // A green must mean "checked and clean", never "found nothing to check".
      // Rename the block and the loop below would iterate an empty set and pass
      // while the sheet sat orphaned — exactly the T-7526 failure mode.
      expect(users.length).toBeGreaterThan(0);
      const offenders = users.filter((f) => !readFileSync(join(DIR, f), "utf8").includes(`"./${sheet}"`));
      expect(offenders).toEqual([]);
    });

    it(`.${block}__* rules live in ${sheet} and nowhere else`, () => {
      // Ownership is only meaningful if the block has ONE home. A rule left
      // behind in (or later added to) another sheet re-creates the free ride:
      // the component's own import would then be insufficient, and deleting it
      // would still leave the surface half-styled instead of visibly broken.
      const strays: string[] = [];
      for (const file of readdirSync(DIR)) {
        if (!file.endsWith(".css") || file === sheet) continue;
        if (new RegExp(`^\\s*\\.${block}[_.:\\s{,]`, "m").test(readFileSync(join(DIR, file), "utf8"))) {
          strays.push(file);
        }
      }
      expect(strays).toEqual([]);
    });
  }
});
