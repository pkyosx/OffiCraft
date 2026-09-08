// The 投入程度 dropdown's ENGLISH option list, written out literally.
//
// 🔴 WHY A LITERAL AND NOT `EFFORTS.map(effortText)`: deriving the expectation
// from the same source the component renders makes the assertion pass for any
// wording. T-131 renamed the fourth level's label from "Extra High" to
// "X-High" and nothing in the repo could tell the difference — the whole round
// of checks stayed green. These five strings are the guard: change a label in
// i18n/locales/en.ts and this list must be changed WITH it, deliberately.
//
// The English locale is the one pinned because it is where the labels have to
// fit: five of them in a phone-width control is what made 投入程度 a dropdown
// in the first place (owner 2026-09-08, card rc-e787f14c9085).

/** The five level slugs, in render order — the closed server vocabulary. */
export const EFFORT_SLUGS = ["low", "medium", "high", "xhigh", "max"] as const;

/** What each level READS AS in English, in render order. */
export const EFFORT_LABELS_EN = [
  "Low",
  "Medium",
  "High",
  "X-High",
  "Max",
] as const;

/** ModelEffortEditor shows the slug beside the label: `Low (low)`. */
export const EFFORT_LABELS_EN_WITH_SLUG = EFFORT_LABELS_EN.map(
  (label, i) => `${label} (${EFFORT_SLUGS[i]})`
);

/** Make the next `render()` paint English. I18nProvider reads this cache on
 * first paint (pre-auth there is nothing else to read), so it must be set
 * BEFORE the component mounts. */
export function useEnglishLocale(): void {
  localStorage.setItem("oc.language", "en");
}

export function clearLocale(): void {
  localStorage.removeItem("oc.language");
}

/** Every option the given `<select>` offers, in DOM order. */
export function optionTexts(select: HTMLElement): string[] {
  return [...select.querySelectorAll("option")].map(
    (o) => o.textContent ?? ""
  );
}

/** Every option's submitted value, in DOM order. */
export function optionValues(select: HTMLElement): string[] {
  return [...select.querySelectorAll("option")].map((o) => o.value);
}
