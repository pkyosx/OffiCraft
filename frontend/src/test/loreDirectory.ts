// test/loreDirectory.ts — the 傳承 directory's remembered folding.
//
// A first visit opens only 所有人 and a folded group renders none of its cards,
// so a spec about something past the directory presets every group open; a spec
// about the folding itself clears the key.

export const LORE_DIR_COLLAPSED_KEY = "oc_lore_dir_collapsed";

export function openEveryLoreDirGroup(): void {
  localStorage.setItem(LORE_DIR_COLLAPSED_KEY, "[]");
}
