/** The text a chat composer opens with when a page links to a chat about one
 * task or 傳承 entry (`#office/chat/<id>/compose/<no>`). */
export function composeSeedFor(no: string): string {
  // owner rc-379631993586: a bare [L-n] does not say what kind of id it is.
  return /^L-\d+$/.test(no) ? `[LoreID=${no}] ` : `[${no}] `;
}
