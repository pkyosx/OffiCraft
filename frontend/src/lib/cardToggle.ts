import type React from "react";

const INTERACTIVE =
  "button, a, textarea, input, select, [role='button'], [role='menu'], [role='dialog'], [role='listbox']";

/** Whether a click on a whole-card toggle (an element carrying role="button"
 * around other controls) is meant to flip the card. A click that lands on or
 * inside any interactive descendant does that control's job instead;
 * `extraSelector` adds a card's own no-toggle zones. The card itself matches
 * [role='button'], so a hit only vetoes when it is NOT the card. A click that
 * ends a drag-selection leaves a non-collapsed selection behind (a plain click
 * collapses any prior one first) — reading is not a toggle intent. */
export function isCardToggleClick(e: React.MouseEvent<HTMLElement>, extraSelector = ""): boolean {
  const target = e.target as HTMLElement;
  const hit = target.closest(extraSelector ? `${INTERACTIVE}, ${extraSelector}` : INTERACTIVE);
  if (hit && hit !== e.currentTarget) return false;
  // isCollapsed over toString(): jsdom implements the former faithfully, and
  // an all-whitespace drag still counts.
  const sel = window.getSelection();
  return !(sel && sel.rangeCount > 0 && !sel.isCollapsed);
}

/** Enter / Space on the card itself; keys bubbling out of a child control do not toggle. */
export function isCardToggleKey(e: React.KeyboardEvent<HTMLElement>): boolean {
  if (e.target !== e.currentTarget) return false;
  if (e.key !== "Enter" && e.key !== " ") return false;
  e.preventDefault();
  return true;
}
