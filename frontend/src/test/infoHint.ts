import { fireEvent, screen, within } from "@testing-library/react";

/** The text behind the ⓘ inside `scope` (the only one there): hovers it, reads
 * the tooltip, and moves the pointer away again. */
export function infoHintText(scope: HTMLElement): string {
  const icon = within(scope).getByTestId("info-hint");
  fireEvent.mouseEnter(icon);
  const text = screen.getByRole("tooltip").textContent ?? "";
  fireEvent.mouseLeave(icon);
  return text;
}
