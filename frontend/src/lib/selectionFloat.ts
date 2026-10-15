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
