/**
 * Auto-grow a one-line-by-default composer <textarea> to fit its content.
 *
 * The chat composer and the reply-card composer are textareas (multi-line:
 * Enter sends, Shift+Enter breaks a line) that must LOOK like the original
 * single-line 44px input when the draft is short, grow with the content, and
 * stop at the CSS max-height (beyond which the textarea's own overflow-y
 * scrolls). The height is content-derived on every draft change: collapse to
 * the CSS min-height first, then take the measured scrollHeight — collapsing
 * first is what lets the box SHRINK back after lines are deleted (an element's
 * scrollHeight never reports smaller than its current height).
 */
export function autosizeTextarea(el: HTMLTextAreaElement): void {
  el.style.height = "auto";
  // scrollHeight includes the padding but not the border — height (border-box,
  // global `box-sizing: border-box`) must, or the content clips by the border
  // width and a phantom scrollbar appears at every size.
  const border = el.offsetHeight - el.clientHeight;
  el.style.height = `${el.scrollHeight + border}px`;
}
