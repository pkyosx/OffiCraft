// Office sidebar 置頂: ONE ordered pin list of member ids spanning both tabs.
// Each tab shows only its own kind's ids, in their relative order; ids of
// members that are not in the list right now are kept, just not shown.

export function orderByPins<T>(
  items: readonly T[],
  pins: readonly string[],
  idOf: (item: T) => string,
): { ordered: T[]; pinnedIds: string[] } {
  const byId = new Map(items.map((it) => [idOf(it), it]));
  const pinnedIds = pins.filter((id) => byId.has(id));
  const pinnedSet = new Set(pinnedIds);
  return {
    ordered: [
      ...pinnedIds.map((id) => byId.get(id)!),
      ...items.filter((it) => !pinnedSet.has(idOf(it))),
    ],
    pinnedIds,
  };
}

/** Put `id` at `index` among the pinned ids its tab shows (`kindPinnedIds`),
 * pinning it if it was not; other tabs' ids keep their places. */
export function pinAt(
  pins: readonly string[],
  id: string,
  index: number,
  kindPinnedIds: readonly string[],
): string[] {
  const rest = pins.filter((p) => p !== id);
  const peers = kindPinnedIds.filter((p) => p !== id);
  const k = Math.max(0, Math.min(index, peers.length));
  if (peers.length === 0) return [...rest, id];
  const at =
    k < peers.length ? rest.indexOf(peers[k]) : rest.indexOf(peers[peers.length - 1]) + 1;
  return [...rest.slice(0, at), id, ...rest.slice(at)];
}

/** Unpin, or pin at the end of the tab's pinned run. */
export function togglePin(
  pins: readonly string[],
  id: string,
  kindPinnedIds: readonly string[],
): string[] {
  return pins.includes(id)
    ? pins.filter((p) => p !== id)
    : pinAt(pins, id, kindPinnedIds.length, kindPinnedIds);
}
