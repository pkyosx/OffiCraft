// check-token-roles.test.ts — the guard's OWN guard (T-081b review round 3).
//
// check-token-roles.mjs asserts the unread badge's WCAG AA story in CI. Round 3
// found three ways to change the product so the badge fails while the script
// still prints "ok": its text colour was never checked, only the FIRST
// background declaration per selector was read (CSS applies the last), and a
// compliant value parked in `@media print` overrode a non-compliant `:root` one
// in the script's eyes but not on screen.
//
// A contrast guard nobody has watched fail is not a guard, so every one of those
// sabotages is replayed here: the real stylesheets are copied to a temp tree,
// ONE of them is edited, and the script must exit non-zero with a message naming
// the problem. TOKEN_ROLES_SRC exists for exactly this.

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { execFileSync } from "node:child_process";
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "check-token-roles.mjs");
const REAL_SRC = join(HERE, "..", "src");

let root: string;

beforeAll(() => {
  root = mkdtempSync(join(tmpdir(), "token-roles-"));
});
afterAll(() => {
  rmSync(root, { recursive: true, force: true });
});

/** Run the guard over a fresh copy of the real stylesheets, after `sabotage`
 *  has had its way with them. Returns the exit code and the combined output. */
function run(sabotage?: (edit: (rel: string, f: (css: string) => string) => void) => void) {
  const src = mkdtempSync(join(root, "src-"));
  cpSync(REAL_SRC, src, { recursive: true });
  sabotage?.((rel, f) => {
    const file = join(src, rel);
    writeFileSync(file, f(readFileSync(file, "utf8")));
  });
  try {
    const stdout = execFileSync("node", [SCRIPT], {
      encoding: "utf8",
      env: { ...process.env, TOKEN_ROLES_SRC: src },
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, out: stdout };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return { code: err.status, out: `${err.stdout}${err.stderr}` };
  }
}

const THEME = "styles/theme.css";
const CHROME = "components/chrome.css";

describe("check-token-roles", () => {
  it("passes on the tree as shipped", () => {
    const { code, out } = run();
    expect(out, out).toContain("[token-roles] ok");
    expect(code).toBe(0);
    // The line must say WHICH token each ratio was measured against — the
    // old wording claimed "3.76:1 on page" while the pill sits on --color-indigo.
    // T-d593 moved the ring off --color-bg onto its own slot, so the name the
    // second ratio is reported against changed with it; a line still saying
    // --color-bg would be describing a measurement the script no longer makes.
    expect(out).toContain("--color-on-danger");
    expect(out).toContain("--color-danger-badge-ring");
    expect(out).toMatch(/office \d+\.\d\d:1 vs --color-on-danger/);
    expect(out).toMatch(/office-light \d+\.\d\d:1 vs --color-on-danger/);
  });

  it("fails when the office-light block's badge fill drops below AA", () => {
    const { code, out } = run((edit) =>
      edit(THEME, (css) => {
        const LIGHT_FILL = "  --color-danger-badge: #b63e35;\n";
        if (!css.includes(LIGHT_FILL)) throw new Error(`fixture is stale: ${THEME} has no ${LIGHT_FILL.trim()}`);
        return css.replace(LIGHT_FILL, "  --color-danger-badge: #f0736b;\n");
      })
    );
    expect(out, out).toMatch(/\[office-light\] --color-danger-badge vs --color-on-danger is 2\.85:1/);
    expect(code).toBe(1);
  });

  it("fails when the office-light block drops on-danger and inherits the dark alias", () => {
    // Without its own value, --color-on-danger falls back to :root's
    // var(--color-overlay), which the light block re-values to a dark ink.
    const { code, out } = run((edit) =>
      edit(THEME, (css) => {
        const LIGHT_TEXT = "  --color-on-danger: #ffffff;\n";
        if (!css.includes(LIGHT_TEXT)) throw new Error(`fixture is stale: ${THEME} has no ${LIGHT_TEXT.trim()}`);
        return css.replace(LIGHT_TEXT, "");
      })
    );
    expect(out, out).toMatch(/\[office-light\] --color-danger-badge vs --color-on-danger is \d/);
    expect(code).toBe(1);
  });

  it("fails when a badge's text colour stops using the measured token", () => {
    const { code, out } = run((edit) =>
      edit(CHROME, (css) =>
        css.replace(
          ".nav-tab__badge {",
          ".nav-tab__badge {\n  color: #8a8a8a;"
        )
      )
    );
    expect(out, out).toMatch(/\.nav-tab__badge's color does not use --color-on-danger/);
    expect(code).toBe(1);
  });

  it("fails when a later declaration re-paints a badge with --color-danger", () => {
    // CSS gives the LAST declaration; reading the first made this invisible.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => `${css}\n.nav-tab__badge { background: var(--color-danger); }\n`)
    );
    expect(out, out).toMatch(
      /\.nav-tab__badge's background does not use --color-danger-badge/
    );
    expect(code).toBe(1);
  });

  it("fails when the badge fill drops below AA in :root, however it is patched elsewhere", () => {
    const { code, out } = run((edit) =>
      edit(THEME, (css) =>
        css.replace("--color-danger-badge: #ba5953;", "--color-danger-badge: #f0736b;") +
        "\n@media print {\n  :root {\n    --color-danger-badge: #ba5953;\n  }\n}\n"
      )
    );
    expect(out, out).toMatch(/--color-danger-badge vs --color-on-danger is 2\.85:1/);
    expect(code).toBe(1);
  });

  it("fails when a badge loses its ring entirely", () => {
    // The shorthand must EXIST, not merely carry the right token: with no
    // outline at all the pill sits directly on --color-indigo (2.74:1) on an
    // active nav tab, and there is no declaration left for the token check to
    // have an opinion about.
    //
    // 🔴 The fixture string is the ring line AS IT IS ON THE TREE TODAY. This
    // test was silently toothless for one commit because it still deleted
    // `outline: 1px solid var(--color-bg);` — the pre-T-d593 text, which no
    // longer occurs, so `css.replace` was a no-op, the guard was handed an
    // UNMODIFIED tree and correctly printed ok. If you re-token the ring, this
    // literal moves with it; a `replace` that matches nothing does not fail, it
    // just stops testing.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => {
        const RING_LINE = "  outline: 1px solid var(--color-danger-badge-ring);\n";
        if (!css.includes(RING_LINE)) throw new Error(`fixture is stale: ${CHROME} has no ${RING_LINE.trim()}`);
        return css.replace(RING_LINE, "");
      })
    );
    expect(out, out).toMatch(/\.nav-tab__badge has no outline declaration/);
    expect(code).toBe(1);
  });

  // ── round 4, SHOULD-B: five ways the guard could be walked past. Every one
  // was measured as exit=0 BEFORE this fix, and every one really renders — two
  // of them were created by the round-3 fix
  // itself, which narrowed the measurement to the literal selector ":root" in
  // the literal file theme.css and so stopped seeing values that DO apply.
  // ── round 4 recheck, SHOULD-1 / SHOULD-2. The guard no longer decides WHICH
  // of several :root definitions wins — it refuses to have more than one. Every
  // shape below used to exit 0 while the screen showed 2.85:1, and each was a
  // different cascade axis the previous model happened not to carry.
  const DUPLICATE = /--color-danger-badge has 2 :root definitions/;

  it("fails when a higher-specificity :root:root also defines the badge fill", () => {
    // Specificity (0,2,0) beats (0,1,0), so the screen showed #f0736b.
    const { code, out } = run((edit) =>
      edit(THEME, (css) => `${css}\n:root:root { --color-danger-badge: #f0736b; }\n`)
    );
    expect(out, out).toMatch(DUPLICATE);
    expect(code).toBe(1);
  });

  it("fails when a DEEPER compound :root:root:root also defines the badge fill", () => {
    // The old model ranked "compound" as one bit, so (0,3,0) and (0,2,0) tied
    // and a non-compliant :root:root:root in theme.css read as compliant.
    const { code, out } = run((edit) =>
      edit(THEME, (css) =>
        css.replace("--color-danger-badge: #ba5953;", "--color-danger-badge: #f0736b;") +
        "\n:root:root { --color-danger-badge: #ba5953; }\n" +
        ":root:root:root { --color-danger-badge: #f0736b; }\n"
      )
    );
    expect(out, out).toMatch(/--color-danger-badge has 3 :root definitions/);
    expect(code).toBe(1);
  });

  it("fails when a badge token's :root definition carries !important", () => {
    // !important was not in the old model at ALL: a non-compliant value marked
    // important in theme.css beat the compliant one in global.css on screen
    // while the guard reported 4.52:1.
    const { code, out } = run((edit) =>
      edit(THEME, (css) =>
        css.replace(
          "--color-danger-badge: #ba5953;",
          "--color-danger-badge: #ba5953 !important;"
        )
      )
    );
    expect(out, out).toMatch(/--color-danger-badge's :root definition carries !important/);
    expect(code).toBe(1);
  });

  it("fails on a second :root definition whichever file holds which value", () => {
    // The old guard hard-coded "theme.css is imported first" in a COMMENT and
    // never read main.tsx, so swapping the two import lines inverted the screen
    // and left the guard's answer byte-identical. It now has no load-order
    // opinion to be wrong about: both directions are refused, so no ordering
    // assumption is load-bearing.
    for (const nonCompliantFile of ["styles/global.css", THEME]) {
      const { code, out } = run((edit) => {
        if (nonCompliantFile === THEME) {
          // theme.css holds the failing value, another sheet the compliant one.
          edit(THEME, (css) =>
            css.replace("--color-danger-badge: #ba5953;", "--color-danger-badge: #f0736b;")
          );
          edit("styles/global.css", (css) =>
            `${css}\n:root { --color-danger-badge: #ba5953; }\n`
          );
        } else {
          // …and the other way round.
          edit(nonCompliantFile, (css) =>
            `${css}\n:root { --color-danger-badge: #f0736b; }\n`
          );
        }
      });
      expect(out, `${nonCompliantFile} → ${out}`).toMatch(DUPLICATE);
      expect(code).toBe(1);
    }
  });

  it("fails when a compound selector re-paints a badge", () => {
    // `.nav-tab__badge.is-hot` matches the same element; `.split(" ").at(-1)`
    // compared the whole compound against ".nav-tab__badge" and skipped it.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => `${css}\n.nav-tab__badge.is-hot { background: var(--color-danger); }\n`)
    );
    expect(out, out).toMatch(
      /\.nav-tab__badge's background does not use --color-danger-badge/
    );
    expect(code).toBe(1);
  });

  it("fails when a selector LIST re-paints a badge", () => {
    // The subject of the FIRST selector is the badge; `.at(-1)` read `.zz`.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => `${css}\n.nav-tab__badge, .zz { background: var(--color-danger); }\n`)
    );
    expect(out, out).toMatch(
      /\.nav-tab__badge's background does not use --color-danger-badge/
    );
    expect(code).toBe(1);
  });

  it("fails when the outline-color longhand removes the badge's ring", () => {
    // The shorthand is still there and still uses the ring slot; the longhand
    // after it is what the browser paints, and the ring is gone.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => `${css}\n.nav-tab__badge { outline-color: transparent; }\n`)
    );
    expect(out, out).toMatch(
      /\.nav-tab__badge's outline-color does not use --color-danger-badge-ring/
    );
    expect(code).toBe(1);
  });

  it("still ignores a compliant value parked in an at-rule", () => {
    // The round-3 fix must survive the round-4 widening: a :root nested in
    // @media is not the screen's truth in EITHER direction.
    const { code, out } = run((edit) =>
      edit(THEME, (css) =>
        css.replace("--color-danger-badge: #ba5953;", "--color-danger-badge: #f0736b;") +
        "\n@media print {\n  :root:root {\n    --color-danger-badge: #ba5953;\n  }\n}\n"
      )
    );
    expect(out, out).toMatch(/--color-danger-badge vs --color-on-danger is 2\.85:1/);
    expect(code).toBe(1);
  });

  it("fails when a badge token is defined outside the theme's :root", () => {
    const { code, out } = run((edit) => {
      edit(THEME, (css) => css.replace("--color-danger-badge: #ba5953;", ""));
      edit(CHROME, (css) => `${css}\n:root {\n  --color-danger-badge: #ba5953;\n}\n`);
    });
    expect(out, out).toMatch(
      /--color-danger-badge is not defined in the :root block of styles\/theme\.css/
    );
    expect(code).toBe(1);
  });
});
