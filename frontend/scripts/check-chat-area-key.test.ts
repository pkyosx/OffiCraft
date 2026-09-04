// check-chat-area-key.test.ts — the guard's OWN guard (T-48, R13-5).
//
// This lint is the ONLY thing that goes red when `<ChatArea>`'s `key` is
// removed, and a dozen behaviours now rest on that key. So the removal is
// replayed here, one branch at a time: the real sources are copied to a temp
// tree, one `key=` is deleted, and the script must exit non-zero naming the
// line. CHAT_AREA_KEY_SRC exists for exactly this.

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "check-chat-area-key.mjs");
const REAL_SRC = join(HERE, "..", "src");

let root: string;

beforeAll(() => {
  root = mkdtempSync(join(tmpdir(), "chat-area-key-"));
});
afterAll(() => {
  rmSync(root, { recursive: true, force: true });
});

function run(
  sabotage?: (edit: (rel: string, f: (code: string) => string) => void) => void,
) {
  const src = mkdtempSync(join(root, "src-"));
  cpSync(REAL_SRC, src, { recursive: true });
  sabotage?.((rel, f) => {
    const file = join(src, rel);
    writeFileSync(file, f(readFileSync(file, "utf8")));
  });
  try {
    const stdout = execFileSync("node", [SCRIPT], {
      encoding: "utf8",
      env: { ...process.env, CHAT_AREA_KEY_SRC: src },
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, out: stdout };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return { code: err.status, out: `${err.stdout}${err.stderr}` };
  }
}

const OFFICE = "components/OfficePage.tsx";

describe("check-chat-area-key", () => {
  it("passes on the tree as shipped, and finds all three mounts", () => {
    const { code, out } = run();
    expect(out, out).toContain("[chat-area-key] ok");
    expect(out).toContain("3 <ChatArea> mounts");
    expect(code).toBe(0);
  });

  it.each([
    ["the member branch", "key={selected.id}"],
    ["the outsource-worker branch", "key={workerPeer.id}"],
    ["the released-peer branch", "key={releasedPeer.id}"],
  ])("reddens when %s loses its key", (_name, keyProp) => {
    const { code, out } = run((edit) =>
      edit(OFFICE, (code) => {
        expect(code).toContain(keyProp);
        return code.replace(keyProp, "");
      }),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("components/OfficePage.tsx");
  });

  it("does not pass vacuously when the component is renamed out from under it", () => {
    // A rule that matches nothing is green. Renaming the component is the edit
    // that would do that silently, so the scan refuses an empty population.
    const { code, out } = run((edit) => {
      edit(OFFICE, (code) => code.replaceAll("<ChatArea", "<ChatRoom"));
      edit("App.tsx", (code) => code); // touch nothing else
    });
    expect(code, out).not.toBe(0);
    expect(out).toContain("no <ChatArea> element found");
  });

  it.each([
    ['a string literal', 'key={selected.id}', 'key="chat"'],
    ['a braced literal', 'key={selected.id}', 'key={"chat"}'],
    ['a bare identifier', 'key={selected.id}', "key={selectedId}"],
  ])("reddens when the key is %s", (_name, from, to) => {
    // A constant key is one instance for every room — the same defect as no key,
    // and the one edit a `has a key=` rule cannot see.
    const { code, out } = run((edit) =>
      edit(OFFICE, (code) => {
        expect(code).toContain(from);
        return code.replace(from, to);
      }),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("does not identify the conversation");
  });

  it("reddens when the key names something this element never mentions", () => {
    const { code, out } = run((edit) =>
      edit(OFFICE, (code) => {
        expect(code).toContain("key={selected.id}");
        return code.replace("key={selected.id}", "key={someRoster.id}");
      }),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("appears nowhere else on this element");
  });

  it("does not count a `<ChatArea>` written inside a comment", () => {
    // Several files spell the component's name in prose. Counting those would
    // make the lint red for a sentence, which is how a guard gets deleted.
    const { code, out } = run((edit) =>
      edit(OFFICE, (code) => `// a bare <ChatArea /> in prose\n${code}`),
    );
    expect(out, out).toContain("[chat-area-key] ok");
    expect(out).toContain("3 <ChatArea> mounts");
    expect(code).toBe(0);
  });
});
