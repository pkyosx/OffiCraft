// seeds.ts derives its exported constants straight from the repo-root seeds/*.md
// single source of truth (imported via `?raw`), applying the one transform the
// server applies at read time ({OWNER_ID} → owner). These tests pin that wiring:
// re-introducing a hand-copied string, breaking the substitution, or emptying a
// seed all turn this suite red. Drift itself is now structurally impossible —
// there is no second copy — so there is nothing left to "keep in sync".

import { describe, it, expect } from "vitest";
import {
  MOCK_OWNER_ID,
  SEED_SYSTEM_INTERACTION_MD,
  SEED_ROLE_ASSISTANT_MD,
  SEED_LESSONS_MD,
  SEED_BOOT_SEQUENCE_MD,
} from "./seeds";

// The same raw sources seeds.ts reads — imported independently here so the
// assertions compare the exported constant against a FRESH read of the file.
import SYSTEM_INTERACTION_RAW from "../../../seeds/system_interaction.md?raw";
import ROLE_ASSISTANT_RAW from "../../../seeds/role_def_assistant.md?raw";
import LESSONS_RAW from "../../../seeds/lessons.md?raw";
import BOOT_SEQUENCE_RAW from "../../../seeds/boot_sequence.md?raw";

const foldOwnerId = (raw: string): string =>
  raw.replace(/\{OWNER_ID\}/g, MOCK_OWNER_ID);

interface SeedCase {
  name: string;
  exported: string;
  raw: string;
}

describe("seeds constants track seeds/*.md by construction", () => {
  const cases: SeedCase[] = [
    { name: "system_interaction", exported: SEED_SYSTEM_INTERACTION_MD, raw: SYSTEM_INTERACTION_RAW },
    { name: "role_def_assistant", exported: SEED_ROLE_ASSISTANT_MD, raw: ROLE_ASSISTANT_RAW },
    { name: "lessons", exported: SEED_LESSONS_MD, raw: LESSONS_RAW },
    { name: "boot_sequence", exported: SEED_BOOT_SEQUENCE_MD, raw: BOOT_SEQUENCE_RAW },
  ];

  it.each(cases)(
    "$name — exported constant equals the raw seed with {OWNER_ID} folded",
    ({ exported, raw }: SeedCase) => {
      // A hand-copied stale string, or any single-char divergence, breaks this.
      expect(exported).toBe(foldOwnerId(raw));
    },
  );

  it.each(cases)(
    "$name — non-empty (guards the empty-vs-empty trap)",
    ({ raw }: SeedCase) => {
      // 空 vs 空 恆相等: a vacuous equality above must not pass on empty strings.
      expect(raw.trim().length).toBeGreaterThan(0);
    },
  );

  it("{OWNER_ID} substitution is real, not a no-op", () => {
    // Positive: the source genuinely carries the placeholder the server folds.
    expect(SYSTEM_INTERACTION_RAW).toContain("{OWNER_ID}");
    // Negative control: the exported constant has none left, and the fold
    // actually produced the owner id in its place.
    expect(SEED_SYSTEM_INTERACTION_MD).not.toContain("{OWNER_ID}");
    expect(SEED_SYSTEM_INTERACTION_MD).toContain(MOCK_OWNER_ID);
  });

  it("system_interaction carries the full current seed, not the old truncated copy", () => {
    // Sections the drifted hand-copy was missing (SSE scrollback, card→node
    // binding). Their presence proves the preview now reflects the real file.
    expect(SEED_SYSTEM_INTERACTION_MD).toContain("before_ts");
    expect(SEED_SYSTEM_INTERACTION_MD).toContain("卡會自動掛到你手上的任務節點");
    expect(SEED_SYSTEM_INTERACTION_MD.length).toBeGreaterThan(5000);
  });
});
