// The T-139 structural guard: NOTHING in the cockpit assembles the terminal
// attach command any more — it arrives finished on the wire and is rendered and
// copied verbatim.
//
// 🔴 WHY A SOURCE SCAN AND NOT A BEHAVIOUR TEST. There were THREE independent
// derivations of this string (the member mapper, the worker panel, and the
// detail panel's copy + display), each correct-looking on its own and each
// wrong on a namespaced station. A behaviour test pins the site it renders; it
// cannot say that a FOURTH one has not appeared somewhere else. The removal of
// `Member.tmuxSession` is the primary guard — with no session name on the type
// there is no ingredient — and this is the second one: it also catches a file
// that hardcodes the whole line, which the type cannot see.
//
// The ONE exception is `api/mock.ts`, which is the stand-in SERVER: composing
// the command is exactly its job there, the same way the real station does it.
// The exception is named, and it is asserted to still be a real match — an
// allowlist nobody checks is how the rule quietly stops applying.

import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const SRC = join(__dirname, "..");
const SERVER_STAND_IN = "api/mock.ts";

/** The socket+attach shape, and a session name being minted from an id. Either
 * one alone is enough to rebuild the command the station is supposed to own. */
const ASSEMBLY = [/tmux\s+-L/, /`member-\$\{/];

function productionSources(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) productionSources(path, out);
    else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name))
      out.push(path);
  }
  return out;
}

/** Comments talk ABOUT the retired pattern — including this ticket's own
 * explanations of it. Only real code counts. */
function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "");
}

function assemblers(): string[] {
  return productionSources(SRC)
    .filter((f) => {
      const code = stripComments(readFileSync(f, "utf8"));
      return ASSEMBLY.some((re) => re.test(code));
    })
    .map((f) => f.slice(SRC.length + 1));
}

describe("terminal attach command ownership", () => {
  it("leaves api/mock.ts — the stand-in server — as the only file that composes one", () => {
    // A dead regex or a mis-rooted walk would pass this vacuously.
    const files = productionSources(SRC);
    expect(files.length).toBeGreaterThan(50);
    expect(files.map((f) => f.slice(SRC.length + 1))).toContain(SERVER_STAND_IN);

    expect(assemblers()).toEqual([SERVER_STAND_IN]);
  });

  it("keeps the allowlisted exception a REAL match, so the rule is still being applied", () => {
    // If mock.ts stops composing the command, this entry must be deleted rather
    // than left standing as a hole the next assembler can walk through.
    const code = stripComments(readFileSync(join(SRC, SERVER_STAND_IN), "utf8"));
    expect(ASSEMBLY.some((re) => re.test(code))).toBe(true);
  });

  it("keeps the session name itself off the client's member/worker types", () => {
    // The ingredient, not the product: with no `tmuxSession` on the domain type
    // there is nothing left to build a command out of, and that is enforced by
    // tsc on every caller rather than by this scan.
    const types = stripComments(readFileSync(join(SRC, "types.ts"), "utf8"));
    const adapter = stripComments(readFileSync(join(SRC, "api/adapter.ts"), "utf8"));
    expect(types).not.toMatch(/\btmuxSession\b/);
    expect(adapter).not.toMatch(/\btmuxSession\b/);
    // …and both DO declare the finished string, so the assertions above are not
    // passing merely because the field was renamed out of existence.
    expect(types).toMatch(/\bterminalAttachCommand\b/);
    expect(adapter).toMatch(/\bterminalAttachCommand\b/);
  });
});
