/** Stable identity for one image in a theme's avatar pool.
 *
 * The id is DERIVED from the image bytes rather than minted, which is what
 * makes a member's selection survive the two paths that would otherwise break
 * it: exporting a theme and importing it elsewhere, and re-saving a theme whose
 * pool was edited. The same image therefore keeps the same id, and removing a
 * DIFFERENT pool item can never rebind a member to it.
 *
 * This is the character-for-character twin of themeIconID in
 * server/ocserverd/avatar_bundle.go: SHA-256 over the whole data URI, the first
 * 6 bytes as hex, prefixed `icn-`. The two MUST agree — the cockpit sends an id
 * the server resolves against its own pool, so a different digest here would
 * 422 every pick.
 */
export const THEME_ICON_ID_PREFIX = "icn-";

export async function themeIconId(image: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(image));
  return (
    THEME_ICON_ID_PREFIX +
    Array.from(new Uint8Array(digest).slice(0, 6))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("")
  );
}

/** Stamp every pool item with its derived id, in place, returning a new map.
 *
 * A caller-supplied id is OVERWRITTEN rather than trusted, for the same reason
 * the server overwrites it: the identity is a function of the bytes, so
 * honouring a caller's id would let one image claim another image's selections.
 */
export async function assignThemeIconIds<K extends string>(
  pools: Partial<Record<K, ({ id?: string; image: string } | string)[]>> | undefined
): Promise<Partial<Record<K, { id: string; image: string }[]>> | undefined> {
  if (!pools) return pools as undefined;
  const out: Record<string, { id: string; image: string }[]> = {};
  for (const [kind, items] of Object.entries(pools) as [
    K,
    ({ id?: string; image: string } | string)[],
  ][]) {
    out[kind] = await Promise.all(
      (items ?? []).map(async (item) => {
        // A stored or imported pool may still be a plain array of data URIs.
        const image = typeof item === "string" ? item : item.image;
        return { id: await themeIconId(image), image };
      })
    );
  }
  return out as Partial<Record<K, { id: string; image: string }[]>>;
}
