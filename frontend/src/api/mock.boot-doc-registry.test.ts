// api/mock.boot-doc-registry.test.ts — pins the cockpit to
// bin/tests/fixtures/boot-doc-registry.tsv; the reasoning lives in its header.
//
// 🔴 THE FAILURE THIS EXISTS FOR IS SILENT. A document ships, the server serves
// it, and the settings page simply has no row for it — no error, no blank page,
// nothing to notice. Every prose list of these kinds ever written in this tree
// went stale exactly that way, and nothing turned red.
//
// THREE HALVES, ONE TABLE. `Record<BootDocKind, …>` makes a kind in the union
// with no row a COMPILE error, and since T-3201 the union itself is pinned to
// the frozen spec's `BootDocKind` enum by the assignment in `toBootDoc` — those
// two halves need no test. This file pins the COCKPIT's list (the settings
// rows, and the mock that stands in for the server in every other frontend
// test) to the SHARED TABLE. The server is checked against that table only for
// has_head, by conformance/test_rest_happy.py. Checking the cockpit against the
// mock alone would only prove the mock agrees with itself: both live in this
// repo half, so both would go stale together the day a document ships on the
// server.
//
// ⚠️ IT NO LONGER READS A LISTING. `GET /api/boot-docs` was removed by the same
// ruling that added the enum: a listing could not go stale but could not make
// anything FAIL either — a cockpit that had never heard of a new document just
// showed nothing. The mock's served set is now read directly.

import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { mockApi, __resetMock, __mockBootDocAddresses } from "./mock";
import { BOOT_DOC_ROWS } from "../components/SettingsPage";
import type { BootDocKind } from "../types";

const TABLE_PATH = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "bin",
  "tests",
  "fixtures",
  "boot-doc-registry.tsv",
);

interface TableRow {
  kind: BootDocKind;
  key: string;
  readOnly: boolean;
  hasHead: boolean;
}

/** Parse the shared table. An unreadable, malformed or EMPTY fixture THROWS —
 * a guard that goes green because it could not read its fixture is a lie, and
 * an empty one would go green by agreeing that no document exists. */
function loadTable(): TableRow[] {
  const rows: TableRow[] = [];
  readFileSync(TABLE_PATH, "utf8").split("\n").forEach((line, i) => {
    if (line.startsWith("#") || line.trim() === "") return;
    const cols = line.split("\t");
    if (cols.length !== 4) {
      throw new Error(`${TABLE_PATH}:${i + 1}: want 4 tab-separated columns, got ${cols.length}`);
    }
    if (cols[0] === "kind") return; // the header row
    (["read_only", "has_head"] as const).forEach((name, n) => {
      if (cols[2 + n] !== "true" && cols[2 + n] !== "false") {
        throw new Error(`${TABLE_PATH}:${i + 1}: ${name} is "${cols[2 + n]}", want true or false`);
      }
    });
    rows.push({
      kind: cols[0] as BootDocKind,
      key: cols[1],
      readOnly: cols[2] === "true",
      hasHead: cols[3] === "true",
    });
  });
  if (rows.length === 0) throw new Error(`${TABLE_PATH} parsed to zero rows`);
  return rows;
}

const TABLE = loadTable();

beforeEach(() => {
  __resetMock();
});

describe("boot-document registry ⇄ settings rows", () => {
  it("gives every document the shared table names a settings row", () => {
    const tableKinds = [...new Set(TABLE.map((r) => r.kind))].sort();
    const rowKinds = (Object.keys(BOOT_DOC_ROWS) as BootDocKind[]).sort();
    expect(rowKinds).toEqual(tableKinds);
  });

  it("serves from the mock exactly the documents the shared table names", async () => {
    // The mock stands in for the server in every other frontend test, so a
    // document missing from it makes those tests pass on a fleet that does not
    // exist. Pinned to the table rather than to the settings rows: two lists in
    // this repo half agreeing with each other proves nothing about the server.
    const served = __mockBootDocAddresses();
    const addr = (r: { kind: string; key: string }) => `${r.kind}/${r.key}`;
    expect(served.map(addr).sort()).toEqual(TABLE.map(addr).sort());
    for (const row of TABLE) {
      const doc = await mockApi.getBootDoc(row.kind, row.key);
      expect(doc.readOnly).toBe(row.readOnly);
      // 🔴 THE HEAD, BOTH WAYS (T-6f44). The mock stands in for the server in
      // every other frontend test, so a stand-in whose documents have a
      // read-only head where the shipped ones no longer do makes those tests
      // green against a fleet that does not exist — which is precisely how the
      // four documents that lost their head this ticket went unnoticed in a
      // suite nobody ran. Empty vs non-empty is all the table claims; the exact
      // text is per-document and stays out of it.
      expect({ addr: `${row.kind}/${row.key}`, hasHead: doc.readOnlyHead !== "" })
        .toEqual({ addr: `${row.kind}/${row.key}`, hasHead: row.hasHead });
    }
  });

  it("addresses each single-key document by the key the server serves it under", () => {
    // A row's `docKey` is what the settings page opens the document WITH, so a
    // row pointing at a key the server does not serve is a 404 the owner meets
    // as a broken page. `boot_sequence` is exempt: it is the one kind serving
    // more than one key, so its row opens an index and the key is chosen there.
    const served = __mockBootDocAddresses();
    for (const doc of served) {
      const row = BOOT_DOC_ROWS[doc.kind];
      if (row.index) {
        expect(doc.kind).toBe("boot_sequence");
        continue;
      }
      expect(row.docKey).toBe(doc.key);
    }
  });

  it("marks as an index exactly the kinds that serve more than one document", () => {
    // The control for the exemption above: if a second kind grew a second key,
    // its row would keep opening one hard-coded document and the other would be
    // unreachable — the same invisibility this file exists to catch.
    const served = __mockBootDocAddresses();
    const keyCount = new Map<BootDocKind, number>();
    for (const doc of served) {
      keyCount.set(doc.kind, (keyCount.get(doc.kind) ?? 0) + 1);
    }
    for (const [kind, count] of keyCount) {
      expect(Boolean(BOOT_DOC_ROWS[kind].index)).toBe(count > 1);
    }
  });
});
