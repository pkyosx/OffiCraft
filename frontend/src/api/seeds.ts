// api/seeds.ts — role-journal file seeds for the mock adapter, imported DIRECTLY
// from the single source of truth (seeds/*.md at the repo root — the very files
// ocserverd go:embeds and serves) so the cockpit preview can NEVER drift from
// what a real agent boots with. There is no hand-maintained copy to re-sync.
//
// Source of truth: seeds/*.md (repo root; read by ocserverd at runtime)
//   * SEED_SYSTEM_INTERACTION_MD ← seeds/system_interaction.md
//   * SEED_ROLE_ASSISTANT_MD     ← seeds/role_def_assistant.md
//   * SEED_LESSONS_MD            ← seeds/lessons.md
//   * SEED_BOOT_SEQUENCE_MD      ← seeds/boot_sequence.md
//
// One transform is applied at read time so the text matches the REAL folded
// GlobalContextDTO.text a client sees: the `{OWNER_ID}` placeholder is
// substituted to MOCK_OWNER_ID ("owner") — the same ReplaceAll the server does
// in readSeedFileFrom (server/ocserverd/assets.go). Importing the files as raw
// text (`?raw`) means no TS template-literal escaping and, crucially, no second
// copy that can go stale.

import SEED_SYSTEM_INTERACTION_RAW from "../../../seeds/system_interaction.md?raw";
import SEED_ROLE_ASSISTANT_RAW from "../../../seeds/role_def_assistant.md?raw";
import SEED_LESSONS_RAW from "../../../seeds/lessons.md?raw";
import SEED_BOOT_SEQUENCE_RAW from "../../../seeds/boot_sequence.md?raw";

/** The out-of-box owner id (mirrors the server seed). */
export const MOCK_OWNER_ID = "owner";

/** Substitute the `{OWNER_ID}` placeholder exactly as ocserverd does at read
 * time (server/ocserverd/assets.go readSeedFileFrom). Global replace; a no-op
 * for seed files that carry no placeholder. */
const foldOwnerId = (raw: string): string =>
  raw.replace(/\{OWNER_ID\}/g, MOCK_OWNER_ID);

/** seeds/system_interaction.md — the read-only 系統互動 block of the boot
 * context, `{OWNER_ID}` substituted to owner. */
export const SEED_SYSTEM_INTERACTION_MD = foldOwnerId(SEED_SYSTEM_INTERACTION_RAW);

/** seeds/role_def_assistant.md — the REAL Mira persona. */
export const SEED_ROLE_ASSISTANT_MD = foldOwnerId(SEED_ROLE_ASSISTANT_RAW);

/** seeds/lessons.md — the REAL accumulated-lessons seed (task_type "general"),
 * is_default=true → the folded GET returns exactly this (UI labels it "預設").
 * An owner edit later overlays it. */
export const SEED_LESSONS_MD = foldOwnerId(SEED_LESSONS_RAW);

/** seeds/boot_sequence.md — the standalone 啟動程序 section appended LAST (after
 * Global → Role → Lessons) so the concrete boot steps are the recency-
 * authoritative tail an agent reads. */
export const SEED_BOOT_SEQUENCE_MD = foldOwnerId(SEED_BOOT_SEQUENCE_RAW);
