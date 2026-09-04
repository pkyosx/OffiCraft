// check-async-landing-points.test.ts — the guard's OWN guard (T-48, R13-6).
//
// The census exists because eleven instances of one defect family got past a
// hand-maintained list. A census nobody has watched fail is worth exactly as
// much as that list was, so every way of getting past it is replayed here: the
// real sources are copied to a temp tree, ONE of them is edited, and the script
// must exit non-zero. ASYNC_LANDING_SRC exists for exactly this.

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "check-async-landing-points.mjs");
const REAL_SRC = join(HERE, "..", "src");

let root: string;

beforeAll(() => {
  root = mkdtempSync(join(tmpdir(), "async-landing-"));
});
afterAll(() => {
  rmSync(root, { recursive: true, force: true });
});

/** Run the guard over a fresh copy of the real sources, after `sabotage` has had
 *  its way with them. Returns the exit code and the combined output. */
function run(
  sabotage?: (
    edit: (rel: string, f: (code: string) => string) => void,
    src: string,
  ) => void,
) {
  // 🔴 EACH RUN GETS ITS OWN PARENT, NOT JUST ITS OWN `src` (T-48, F-D). The
  // path-alias case writes a `tsconfig.json` at `src/..`, and while every run
  // shared one parent that file stayed there and poisoned every LATER run. It
  // went unnoticed for as long as every later case asserted only "non-zero" —
  // the first assertion that the census is CLEAN is the first one it broke.
  const src = join(mkdtempSync(join(root, "run-")), "src");
  cpSync(REAL_SRC, src, { recursive: true });
  sabotage?.((rel, f) => {
    const file = join(src, rel);
    writeFileSync(file, f(readFileSync(file, "utf8")));
  }, src);
  try {
    const stdout = execFileSync("node", [SCRIPT], {
      encoding: "utf8",
      env: { ...process.env, ASYNC_LANDING_SRC: src },
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, out: stdout };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return { code: err.status, out: `${err.stdout}${err.stderr}` };
  }
}

const CHAT_AREA = "components/ChatArea.tsx";

describe("check-async-landing-points", () => {
  it("passes on the tree as shipped", () => {
    const { code, out } = run();
    expect(out, out).toContain("[async-landing] ok");
    expect(code).toBe(0);
  });

  it("reddens when a NEW landing point appears with no verdict", () => {
    // The whole point: a new `setTimeout` in ChatArea is an unanswered question
    // until somebody writes down what happens if it commits after the owner has
    // left the room.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          "  const isComposingRef = useRef(false);",
          "  const isComposingRef = useRef(false);\n  setTimeout(() => {}, 0);",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("components/ChatArea.tsx | setTimeout/setInterval");
  });

  it("reddens when a landing point DISAPPEARS, so the register cannot describe code that is gone", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useAttachmentStaging.ts", (code) =>
        code.replace(
          "const reader = new FileReader();",
          "const reader = fr();",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useAttachmentStaging.ts | FileReader");
  });

  it("reddens on a SECOND occurrence appended to an already-counted line (R10-5 B5)", () => {
    // The census counts occurrences, not lines. It used to count lines, so a
    // second `await` on a line that already had one was free.
    const { code, out } = run((edit) =>
      edit("lib/shareLink.ts", (code) =>
        code.replace("await ", "await await "),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("lib/shareLink.ts | await");
  });

  it("reddens when a SECOND module-level per-room table grows outside the draft store (R14-3.1)", () => {
    // The instance that actually happened: `liveComposers`, a peer-keyed table
    // in ChatArea.tsx rather than in chatDraftStore.ts. The store's own test
    // cannot see it — it reads the store's file. This walks the whole graph.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          "export function ChatArea({",
          "const liveComposers = new Map<string, () => void>();\nexport function ChatArea({",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("components/ChatArea.tsx | liveComposers");
  });

  it("reddens when registered module-level state disappears, so the register cannot describe a table that is gone", () => {
    const { code, out } = run((edit) =>
      edit("lib/chatDraftStore.ts", (code) =>
        code.replace("const drafts = new Map", "const draftsById = new Map"),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain(
      "registered module-level state that no longer exists",
    );
    expect(out).toContain("lib/chatDraftStore.ts | drafts");
  });

  // 🔴 THE FIVE WAYS ROUND THE OLD REGEX CENSUS (T-48, R16 D-3). Every case
  // below was measured GREEN by the sixteenth review against the regex version
  // and is red here because both censuses now read the AST. They are kept as
  // separate cases rather than folded together because each one is a different
  // author writing the same table in the spelling that came naturally.
  describe.each([
    [
      "an array-literal table",
      "const liveComposers: { peerId: string; repaint: () => void }[] = [];",
    ],
    [
      "an object-literal table",
      "const liveComposers: Record<string, () => void> = {};",
    ],
    [
      "a Map seeded from an expression",
      "const liveComposers = new Map<string, () => void>(Object.entries({}));",
    ],
    [
      "the registered shape with a trailing comment",
      "const liveComposers = new Map<string, () => void>(); // per-room repaint table",
    ],
  ])("a second per-room table written as %s", (_name, decl) => {
    it("reddens", () => {
      const { code, out } = run((edit) =>
        edit(CHAT_AREA, (code) =>
          code.replace(
            "export function ChatArea({",
            `${decl}\nexport function ChatArea({`,
          ),
        ),
      );
      expect(code, out).not.toBe(0);
      expect(out).toContain("components/ChatArea.tsx | liveComposers");
    });
  });

  it("reddens on a per-room table mutated THROUGH a getter, the fifth evasion (R17 A-7)", () => {
    // The seventeenth review measured this one green. It defeats both of the
    // clauses that would otherwise catch it, and it does so honestly rather than
    // by spelling: the seed is non-empty, so "an empty container is a promise to
    // write to it" does not fire; and the receiver of `.push` is a CallExpression
    // under a `!`, so the write-through clause — which used to require a bare
    // identifier there — did not see a name to attribute it to.
    //
    // It matters more than the four shapes already listed as out of reach,
    // because it is the one a person would REACH FOR: seeding a table and then
    // appending into a room's slot is ordinary code, not a way around a check.
    // It was also missing from the ceiling list, which made the list itself
    // misleading — four named shapes read as the whole boundary.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          "export function ChatArea({",
          'const roomBuf = new Map<string, string[]>([["seed", []]]);\n' +
            "function stashForRoom(peer: string, v: string) { roomBuf.get(peer)!.push(v); }\n" +
            "export function ChatArea({",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("components/ChatArea.tsx | roomBuf");
  });

  it("reddens on a per-room table in src/api, which the state census walks and the await census does not (R16 D-3)", () => {
    // The api exclusion was inherited from the AWAIT census, whose reason is
    // about counting 126 unrelated awaits. That reason says nothing about
    // whether one room's value can reach another, and an SSE transport is the
    // likeliest place for a per-room cache to be put.
    const { code, out } = run((edit) =>
      edit("api/http.ts", (code) =>
        code.replace(
          "let sseSource",
          "const lastSeenPerRoom = new Map<string, string>();\nlet sseSource",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("api/http.ts | lastSeenPerRoom");
  });

  it("reddens when a registered module-level array is renamed, so escapeLayers' Esc stack cannot quietly leave the register (R16 D-3)", () => {
    // `const layers: Layer[] = []` was module-level mutable state sitting in the
    // walk, beside a sibling (`let listening`) that WAS registered, and the
    // regex census could not see an array literal at all.
    const { code, out } = run((edit) =>
      edit("lib/escapeLayers.ts", (code) =>
        code.replace(/\blayers\b/g, "layerStack"),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("lib/escapeLayers.ts | layers");
  });

  it("reddens when a path alias the walk cannot resolve is configured (R16 D-3)", () => {
    // `resolveSpec` returns null for every non-relative specifier. That is right
    // for `react` and wrong for `@/lib/x`: files imported through an alias would
    // leave the population in silence, which is the failure this census has
    // already been bitten by twice.
    const { code, out } = run((_edit, src) => {
      writeFileSync(
        join(src, "..", "tsconfig.json"),
        JSON.stringify({ compilerOptions: { paths: { "@/*": ["./src/*"] } } }),
      );
    });
    expect(code, out).not.toBe(0);
    expect(out).toContain("path aliases the walk cannot resolve");
  });

  // Rule 7 — the thread's setter has ONE home. Both mutants are second thread
  // states in `useChat`; the second carries NO type annotation at all, which is
  // what the previous, text-matching version of the rule let through (census
  // green, tsc green — independent review F2).
  it("reddens on a second useState<Thread> outside lib/threadCommit.ts", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useChat.ts", (code) =>
        code.replace(
          "  const [peerLastReadTs, setPeerLastReadTs] = useState(0);",
          "  const [peerLastReadTs, setPeerLastReadTs] = useState(0);\n" +
            '  const [t2, setT2] = useState<import("../lib/threadCommit").Thread>({ messages: [], hasMore: true, gapSuspected: false, hasNewer: false });\n' +
            "  void t2; void setT2;",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useChat.ts declares the chat thread's own state");
  });

  it("reddens on a second thread state that never spells the word Thread", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useChat.ts", (code) =>
        code.replace(
          "  const [peerLastReadTs, setPeerLastReadTs] = useState(0);",
          "  const [peerLastReadTs, setPeerLastReadTs] = useState(0);\n" +
            "  const EMPTY2 = { messages: [] as ChatMessage[], hasMore: true, gapSuspected: false, hasNewer: false };\n" +
            "  const [xthread, setXThread] = useState(EMPTY2);\n" +
            "  const paintRaw = (msgs: ChatMessage[]) => setXThread((p) => ({ ...p, messages: msgs }));\n" +
            "  void xthread; void paintRaw;",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useChat.ts declares the chat thread's own state");
  });

  // 🔴 THE CALLEE HALF OF RULE 7, WHICH USED TO BE A LITERAL NAME TEST (F-D).
  // The rule's message promised it fires "however the shape is spelled". These
  // three ordinary spellings all passed it — measured on a temp tree with the
  // un-annotated mutant above as the positive control. Each names the SAME
  // second thread setter; only the way the hook is reached differs.
  const SECOND_THREAD =
    "  const EMPTY2 = { messages: [] as ChatMessage[], hasMore: true, gapSuspected: false, hasNewer: false };\n";
  const AFTER = "  const [peerLastReadTs, setPeerLastReadTs] = useState(0);";

  it("reddens on a second thread state reached as React.useState", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useChat.ts", (code) =>
        'import * as React from "react";\n' +
          code.replace(
            AFTER,
            AFTER +
              "\n" +
              SECOND_THREAD +
              "  const [xthread, setXThread] = React.useState(EMPTY2);\n" +
              "  void xthread; void setXThread;",
          ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useChat.ts declares the chat thread's own state");
  });

  it("reddens on a second thread state reached through a local alias of useState", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useChat.ts", (code) =>
        code.replace(
          AFTER,
          AFTER +
            "\n" +
            SECOND_THREAD +
            "  const us = useState;\n" +
            "  const [xthread, setXThread] = us(EMPTY2);\n" +
            "  void xthread; void setXThread;",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useChat.ts declares the chat thread's own state");
  });

  it("reddens on a second thread state reached through a renamed import of useState", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useChat.ts", (code) =>
        'import { useState as useStore } from "react";\n' +
          code.replace(
            AFTER,
            AFTER +
              "\n" +
              SECOND_THREAD +
              "  const [xthread, setXThread] = useStore(EMPTY2);\n" +
              "  void xthread; void setXThread;",
          ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useChat.ts declares the chat thread's own state");
  });

  // 🔴 THE FLOOR UNDER THE RESOLUTION (independent review A-1). A barrel that
  // re-exports the hook under its own name resolves to a declaration rule 7
  // does not recognise — not to nothing — so a floor that only ran when
  // NOTHING resolved left this second setter green, while the literal test it
  // replaced reddened it. The floor is ORed with the resolution for that.
  it("reddens on a second thread state reached through a local re-export barrel", () => {
    const { code, out } = run((edit, src) => {
      writeFileSync(
        join(src, "hooks", "reactCompat.ts"),
        'export { useState } from "react";\n',
      );
      edit("hooks/useChat.ts", (code) =>
        code
          .replace(
            'import { useCallback, useEffect, useRef, useState } from "react";',
            'import { useCallback, useEffect, useRef } from "react";\n' +
              'import { useState } from "./reactCompat";',
          )
          .replace(
            AFTER,
            AFTER +
              "\n" +
              SECOND_THREAD +
              "  const [xthread, setXThread] = useState(EMPTY2);\n" +
              "  void xthread; void setXThread;",
          ),
      );
    });
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useChat.ts declares the chat thread's own state");
  });

  // 🟠 THE RESIDUE, ASSERTED AS A RESIDUE. Renaming the property is the one of
  // review F-D's four spellings that still passes, and it passes ON PURPOSE:
  // firing on any property that holds ChatMessage[] would redden every list in
  // the tree. The rule's message names this; this test is what stops the
  // message and the behaviour drifting apart.
  it("does NOT redden on a thread-shaped state whose property is not named messages", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useChat.ts", (code) =>
        code.replace(
          AFTER,
          AFTER +
            "\n" +
            "  const EMPTY3 = { rows: [] as ChatMessage[], hasMore: true, gapSuspected: false, hasNewer: false };\n" +
            "  const [xthread3, setXThread3] = useState(EMPTY3);\n" +
            "  void xthread3; void setXThread3;",
        ),
      ),
    );
    expect(code, out).toBe(0);
    expect(out).toContain("[async-landing] ok");
  });

  // 🟠 THE CALLEE HALF, ASSERTED AS NON-VACUOUS. `reactHookOf` returning null
  // everywhere leaves rule 7 matching no call site at all, and a rule matching
  // nothing passes every file — which is how the hand-maintained list this
  // census replaced used to die. Rename every hook in the tree and the script
  // must say so rather than print ok.
  it("reddens when no callee in the walk resolves to a React hook, instead of passing that half vacuously", () => {
    const { code, out } = run((_edit, src) => {
      const renameHooks = (dir: string) => {
        for (const entry of readdirSync(dir, { withFileTypes: true })) {
          const p = join(dir, entry.name);
          if (entry.isDirectory()) renameHooks(p);
          else if (/\.tsx?$/.test(entry.name))
            writeFileSync(
              p,
              readFileSync(p, "utf8").replace(
                /\buse(State|Reducer|Ref)\b/g,
                "useZ$1",
              ),
            );
        }
      };
      renameHooks(src);
    });
    expect(code, out).not.toBe(0);
    expect(out).toContain("rule 7 resolved NO callee anywhere in the walk");
  });

  it("reddens when a SECOND component calls useQuotedMessageOverlay (R14-1.6)", () => {
    // The overlay carries no room stamp of its own: it relies on ChatArea being
    // unmounted by a room switch. A caller keyed on a card id is not.
    const { code, out } = run((edit) =>
      edit("components/ReplyComposer.tsx", (code) =>
        code.replace(
          'import { useI18n } from "../i18n";',
          'import { useI18n } from "../i18n";\nimport { useQuotedMessageOverlay } from "../hooks/useQuotedMessageOverlay";\nconst _q = () => useQuotedMessageOverlay((id: string) => id);',
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("useQuotedMessageOverlay's callers changed");
    expect(out).toContain("components/ReplyComposer.tsx");
  });

  // The caller census was a SUBSTRING match on `useQuotedMessageOverlay(`, so a
  // rename or a line break walked straight past it (R16 D-3). Each spelling
  // below is the same second caller.
  describe.each([
    [
      "a renamed import",
      'import { useQuotedMessageOverlay as useOverlay } from "../hooks/useQuotedMessageOverlay";\nconst _q = () => useOverlay((id: string) => id);',
    ],
    [
      "a line break before the argument list",
      'import { useQuotedMessageOverlay } from "../hooks/useQuotedMessageOverlay";\nconst _q = () => useQuotedMessageOverlay\n  ((id: string) => id);',
    ],
    [
      "a namespace import",
      'import * as Q from "../hooks/useQuotedMessageOverlay";\nconst _q = () => Q.useQuotedMessageOverlay((id: string) => id);',
    ],
  ])("a second caller reached through %s", (_name, snippet) => {
    it("reddens", () => {
      const { code, out } = run((edit) =>
        edit("components/ReplyComposer.tsx", (code) =>
          code.replace(
            'import { useI18n } from "../i18n";',
            `import { useI18n } from "../i18n";\n${snippet}`,
          ),
        ),
      );
      expect(code, out).not.toBe(0);
      expect(out).toContain("useQuotedMessageOverlay's callers changed");
      expect(out).toContain("components/ReplyComposer.tsx");
    });
  });

  it("reddens when a second caller reaches the hook through a renamed re-export barrel", () => {
    // A re-export chain is the one indirection a per-file import scan misses;
    // the binding resolution below is a fixpoint for exactly this reason.
    const { code, out } = run((edit, src) => {
      writeFileSync(
        join(src, "hooks", "overlayBarrel.ts"),
        'export { useQuotedMessageOverlay as useOverlay } from "./useQuotedMessageOverlay";\n',
      );
      edit("components/ReplyComposer.tsx", (code) =>
        code.replace(
          'import { useI18n } from "../i18n";',
          'import { useI18n } from "../i18n";\nimport { useOverlay } from "../hooks/overlayBarrel";\nconst _q = () => useOverlay((id: string) => id);',
        ),
      );
    });
    expect(code, out).not.toBe(0);
    expect(out).toContain("useQuotedMessageOverlay's callers changed");
  });

  it("reddens when a file drops out of the WALK (R10-5 B1/B2/B3)", () => {
    // The population is derived from ChatArea's imports. A file that stops being
    // reachable takes its landing points with it, silently, unless the register
    // notices the rows are gone.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          'import { ChatGalleryPanel } from "./ChatGalleryPanel";',
          "const ChatGalleryPanel = (() => null) as never;",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("ChatGalleryPanel.tsx");
  });
});
