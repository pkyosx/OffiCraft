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
    // The line must say WHICH background each ratio was measured against — the
    // old wording claimed "3.76:1 on page" while the pill sits on --color-indigo.
    expect(out).toContain("--color-on-danger");
    expect(out).toContain("--color-bg");
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

  it("fails when a badge loses its page-colour ring", () => {
    // The ring is what makes the 3:1-vs-page number true: without it the pill
    // sits on --color-indigo (2.74:1) on an active nav tab.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => css.replace("  outline: 1px solid var(--color-bg);\n", ""))
    );
    expect(out, out).toMatch(/\.nav-tab__badge has no outline declaration/);
    expect(code).toBe(1);
  });

  // ── round 4, SHOULD-B: five ways the guard could be walked past. Every one
  // was measured as exit=0 BEFORE this fix (round4-review/guard-bypass-probe.md)
  // and every one really renders — two of them were created by the round-3 fix
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
    // The shorthand is still there and still uses --color-bg; the longhand after
    // it is what the browser paints, and the ring is gone.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => `${css}\n.nav-tab__badge { outline-color: transparent; }\n`)
    );
    expect(out, out).toMatch(
      /\.nav-tab__badge's outline-color does not use --color-bg/
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

  // ── the 任務 count's own rules (T-2658) ──
  // Its whole reason for having a separate class is that a modifier on
  // .nav-tab__badge would have been a compound bypass of the rules above. That
  // only buys anything if the new class is itself watched failing.

  it("fails when the 任務 count is re-painted with the alert fill", () => {
    const { code, out } = run((edit) =>
      edit(
        CHROME,
        (css) => `${css}\n.nav-tab__count { background: var(--color-danger-badge); }\n`
      )
    );
    expect(out, out).toMatch(
      /\.nav-tab__count's background resolves to --color-danger-badge/
    );
    expect(code).toBe(1);
  });

  it("fails when the 任務 count's fill stops following the theme's overlay base", () => {
    // A fixed colour looks right in the built-in dark theme and wrong on every
    // light pack — the exact failure mode this whole file exists for.
    const { code, out } = run((edit) =>
      edit(CHROME, (css) =>
        css.replace(
          "  background: color-mix(in srgb, var(--color-overlay) 13%, transparent);",
          "  background: var(--color-card);"
        )
      )
    );
    expect(out, out).toMatch(
      /\.nav-tab__count's background does not resolve to --color-overlay/
    );
    expect(code).toBe(1);
  });

  it("fails when the 任務 count loses its hairline", () => {
    const { code, out } = run((edit) =>
      edit(CHROME, (css) => `${css}\n.nav-tab__count { outline-color: transparent; }\n`)
    );
    expect(out, out).toMatch(
      /\.nav-tab__count's outline-color does not resolve to --color-overlay/
    );
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
