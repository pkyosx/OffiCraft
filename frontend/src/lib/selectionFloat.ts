const MARGIN_PX = 8;

/** Where a floating action for a text selection goes: centred under the
 * selection — above it is where the line before sits and where iOS shows its
 * Copy/Look Up menu — flipped above only when below would leave the viewport,
 * and clamped inside the viewport horizontally. */
export function placeUnderSelection(
  selection: DOMRect,
  float: { width: number; height: number },
): { left: number; top: number } {
  const { clientWidth, clientHeight } = document.documentElement;
  const left = Math.min(
    Math.max(selection.left + selection.width / 2 - float.width / 2, MARGIN_PX),
    Math.max(clientWidth - float.width - MARGIN_PX, MARGIN_PX),
  );
  const below = selection.bottom + MARGIN_PX;
  const top =
    below + float.height <= clientHeight - MARGIN_PX
      ? below
      : Math.max(MARGIN_PX, selection.top - float.height - MARGIN_PX);
  return { left, top };
}

/** `range` cut down to `container`, or null when that would drop visible text
 * or leave none. A triple-click ends the range at offset 0 of whatever follows
 * the paragraph, which for a container's last block lies outside it. */
export function rangeInside(range: Range, container: Node): Range | null {
  const startIn = container.contains(range.startContainer);
  const endIn = container.contains(range.endContainer);
  if (!startIn && !endIn) return null;
  const inside = range.cloneRange();
  if (!startIn) inside.setStart(container, 0);
  if (!endIn) inside.setEnd(container, container.childNodes.length);
  const text = inside.toString().trim();
  return text !== "" && text === range.toString().trim() ? inside : null;
}
