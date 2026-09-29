// The wording cap has to clear the message-key whitelist, and this side asserts
// it because this is the side that used to go red for the wrong reason: when the
// cap sat at or below the whitelist length, what failed was ThemeSettings.test.tsx
// (forging a pack of "every whitelisted key + 1") with `expected 1 to be 2` — a
// message about theme rows that says nothing about a cap.
//
// Nothing compares this constant with its Go twin (maxWordingEntriesPerLang in
// server/ocserverd/wording_bundle.go).
import { describe, it, expect } from "vitest";
import { MAX_WORDING_ENTRIES_PER_LANG } from "./themeBundleCore";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";

const WANT_SPARE = 50;

describe("MAX_WORDING_ENTRIES_PER_LANG", () => {
  it("leaves room above the message-key whitelist", () => {
    // A guard that passes because its corpus is empty proves nothing.
    expect(MESSAGE_KEYS.length).toBeGreaterThan(0);

    const headroom = MAX_WORDING_ENTRIES_PER_LANG - MESSAGE_KEYS.length;
    if (headroom < WANT_SPARE) {
      throw new Error(
        `THE WORDING CAP HAS RUN OUT OF ROOM ABOVE THE MESSAGE-KEY WHITELIST.

  message-key whitelist  ${MESSAGE_KEYS.length} keys   (src/i18n/locales/en.ts, via npm run gen:msgkeys)
  wording entry cap      ${MAX_WORDING_ENTRIES_PER_LANG}        (the most raw entries one language may submit)
  headroom               ${headroom}        (want at least ${WANT_SPARE})

validateWording counts the RAW submitted entries BEFORE unknown codes are
pruned, so a legitimate pack that re-words EVERY message key submits exactly
${MESSAGE_KEYS.length} entries. Once the cap stops clearing the whitelist, that pack is
refused by its own validator and theme wording is broken for everyone.

THE FIX IS TO RAISE BOTH TWINS TOGETHER, in one commit, to the same number:

  frontend/src/lib/themeBundleCore.ts  MAX_WORDING_ENTRIES_PER_LANG
  server/ocserverd/wording_bundle.go   maxWordingEntriesPerLang

Nothing checks that the two are equal: if you raise only this one, this test
goes green while the server keeps its old cap.`
      );
    }
  });
});
