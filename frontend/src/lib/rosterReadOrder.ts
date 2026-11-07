// lib/rosterReadOrder.ts — decides what an overlapping roster read may still
// write, by the order the reads were ISSUED. Response order says nothing: the
// read issued after a delta is the one guaranteed to see what the delta reported.
//
// ⚠️ This is deliberately not useMachines' "only the last issued read may land".
// Here a row read can overtake a list, and the list still holds the only news
// about every other row, so it must land around the newer row. And if the newest
// read then fails, the older answer already on screen is the best one we have.

type RosterUpdate<T> = (rows: T[]) => T[];

export interface RosterReadOrder<T extends { id: string }> {
  /** Call exactly when a request goes out. */
  begin(): number;
  /** The state update for a list response, or null when a list issued later has
   * already landed — that list answers everything this one could. */
  landList(read: number, list: T[]): RosterUpdate<T> | null;
  /** The state update for single-row responses. */
  landRows(read: number, fresh: T[]): RosterUpdate<T>;
  /** True once any list has landed. */
  listHasLanded(): boolean;
}

export function createRosterReadOrder<
  T extends { id: string },
>(): RosterReadOrder<T> {
  let lastIssued = 0;
  let listLanded = 0;
  const rowLanded = new Map<string, number>();

  return {
    begin: () => ++lastIssued,

    landList(read, list) {
      if (read < listLanded) return null;
      listLanded = read;
      const keepCurrent = new Set<string>();
      for (const [id, rowRead] of rowLanded) {
        if (rowRead > read) keepCurrent.add(id);
        else rowLanded.delete(id);
      }
      return (rows) =>
        list.map((row) =>
          keepCurrent.has(row.id)
            ? (rows.find((r) => r.id === row.id) ?? row)
            : row
        );
    },

    landRows(read, fresh) {
      const accepted = fresh.filter(
        (row) => read > Math.max(listLanded, rowLanded.get(row.id) ?? 0)
      );
      for (const row of accepted) rowLanded.set(row.id, read);
      return (rows) =>
        rows.map((row) => accepted.find((a) => a.id === row.id) ?? row);
    },

    listHasLanded: () => listLanded > 0,
  };
}
