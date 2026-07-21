// dispatchAlert · 文案的教義 (T-7fa1, review r2 BLOCKER-2).
//
// 🔴 WHY A TEST ABOUT WORDS. Two review rounds have now found the SAME defect in
// this one paragraph: it asserted a cause the server's bool cannot support.
//   r1 — wakeBody: "指令沒有送達目標機器" / "Nothing reached the target machine".
//        `activation_pending` is a NEGATIVE catch-all (`dec.Command != start &&
//        !IsOnline`); it also fires during retry backoff, circuit-open, an
//        unbuildable START frame, and while a PREVIOUS start is still in flight,
//        where that sentence is false AND contradicts 「最近操作」 on the same panel.
//   r2 — relocateBody: "成員還在原本的機器上" / "still on its old machine".
//        A server probe showed `relocation_pending` firing while the member is on
//        NO machine (`running == ""`), with the same panel's 機器 cell showing 「—」.
//
// The rework for r1 was rewritten prose and NOTHING ELSE — the reviewer pasted
// the old lying sentence back in and all 908 tests stayed green, and no test had
// ever read the `en` or `xian` dictionaries at all. So the one thing this ticket
// exists to protect had no guard, in any language.
//
// 🔴 WHAT THIS FILE ACTUALLY GUARDS — named after what it does, not after what
// we wish it did (review r3 measured the gap; an earlier version of this header
// claimed to "encode the DOCTRINE", which is more than it can deliver):
//
//   1. A BLOCKLIST of the two lies above. Each is banned by the substrings it
//      is made of, in all three locales, so the copy may be rewritten freely
//      but cannot drift back into either historical PHRASING. Note the word:
//      phrasing, not claim. Re-word the same false claim around the banned
//      substrings and it sails through (zh bans 送達/目標機器/沒有送到 —
//      「指令沒有抵達那台機器」 passes). That is the same gap as the novel-lie
//      one below, and naming it "claim" here would be this very file's defect.
//   2. A STRUCTURAL invariant: the copy must keep pointing at 「最近操作」, and
//      that pointer is cross-checked against the same locale's `mp.lastOp`
//      rather than a hard-coded string. This one is a real invariant.
//
// 🔴 WHAT IT DOES NOT CATCH: a NOVEL lie. r3 wrote a fresh false cause that
// avoids every banned substring and this suite stayed green (928/928). A
// reviewer's eyes are still the only thing standing between the copy and a
// cause the flag does not know. Do not read a green run here as "the copy is
// honest" — read it as "the copy has not regressed to a KNOWN lie".
//
// Deliberately NOT asserted: exact sentences. A test that pins the prose blocks
// every improvement and catches no lie.

import { describe, it, expect } from "vitest";
import { zh } from "./locales/zh";
import { en } from "./locales/en";
import { xian } from "./locales/xian";
import type { Dict } from "./locales/zh";

interface Case {
  name: string;
  dict: Dict;
  /** Phrasings that name a delivery failure the wake flag cannot see. Every one
   * of these is lifted from the copy r1 判定為謊 (commit a3b2a8e). */
  wakeBannedCause: string[];
  /** Phrasings that assert WHERE the member is — what r2 disproved by probe. */
  relocateBannedPlacement: string[];
}

const CASES: Case[] = [
  {
    name: "zh",
    dict: zh,
    // a3b2a8e: "指令沒有送達目標機器，這位成員不會醒過來。…"
    wakeBannedCause: ["送達", "目標機器", "沒有送到"],
    // a3b2a8e + 8590ba5: "…成員還在原本的機器上。"
    relocateBannedPlacement: ["還在原本的機器", "仍在原本的機器", "還在原機"],
  },
  {
    name: "en",
    dict: en,
    // a3b2a8e: "Nothing reached the target machine, so this member will not wake up. …"
    wakeBannedCause: ["target machine", "never delivered", "reached the"],
    // a3b2a8e + 8590ba5: "… the member is still on its old machine."
    relocateBannedPlacement: ["still on its old", "still on the old"],
  },
  {
    name: "xian",
    dict: xian,
    // a3b2a8e: "訣令未達機樞，此侍者不會轉醒。…"
    wakeBannedCause: ["未達", "機樞未"],
    // a3b2a8e + 8590ba5: "…侍者仍在舊處。"
    relocateBannedPlacement: ["仍在舊", "尚在舊"],
  },
];

describe("dispatchAlert copy · the notice may not out-claim its bool (T-7fa1)", () => {
  for (const c of CASES) {
    const a = c.dict.dispatchAlert;

    it(`${c.name}: wakeBody names no cause the flag cannot see (review r1 BLOCKER-1)`, () => {
      for (const banned of c.wakeBannedCause) {
        expect(
          a.wakeBody,
          `wakeBody must not claim "${banned}" — activation_pending is a ` +
            `negative catch-all and fires on paths where that is false`,
        ).not.toContain(banned);
      }
    });

    it(`${c.name}: the wake copy hands the owner back to 最近操作 (review r1 BLOCKER-1)`, () => {
      // The one place a PRECISE reason exists when the flag fired after a START
      // that WAS dispatched (`last_op_reason`). Cross-referenced against the
      // SAME locale's label so the pointer cannot drift out of the panel it
      // points at — 「最近操作」/"Last operation"/「最近施法」 are all different, and
      // `workerDetail` carries a same-named key with a DIFFERENT value.
      const label = c.dict.mp.lastOp;
      expect(label.length, "the panel label must be non-empty").toBeGreaterThan(0);
      const wakeCopy = [a.wakeBody, a.wakeStep1, a.wakeStep2].join(" ");
      expect(wakeCopy).toContain(label);
    });

    it(`${c.name}: relocateBody asserts nothing about WHERE the member is (review r2 BLOCKER-1)`, () => {
      for (const banned of c.relocateBannedPlacement) {
        expect(
          a.relocateBody,
          `relocateBody must not claim "${banned}" — relocation_pending also ` +
            `fires while the member is on no machine at all`,
        ).not.toContain(banned);
      }
    });

    it(`${c.name}: the two kinds do not share their steps`, () => {
      // relocation_pending is a POSITIVE determination (a decided command the
      // addressed warden refused), activation_pending is not. One set of steps
      // for both means one of them is either over- or under-claiming — which is
      // how the relocate half ended up telling the owner LESS than its flag knew.
      expect(a.relocateStep1).not.toBe(a.wakeStep1);
      expect(a.relocateStep2).not.toBe(a.wakeStep2);
      for (const s of [
        a.wakeTitle,
        a.wakeBody,
        a.wakeStep1,
        a.wakeStep2,
        a.relocateTitle,
        a.relocateBody,
        a.relocateStep1,
        a.relocateStep2,
      ]) {
        expect(s.trim().length).toBeGreaterThan(0);
      }
    });
  }
});
