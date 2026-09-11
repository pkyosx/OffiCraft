// Which fields a retained revision SHOWS, per document kind.
//
// A mutant sweep found this table unguarded: widening `task_manual_sop` to also
// list `purpose` left 55 tests across the modal / entry / settings suites
// green, because every one of them feeds a revision that carries only the field
// it expects. The table is exactly where the owner's two de-versioning rulings
// land (手冊拆包 2026-07-31, 角色名稱退出版控 2026-07-31), so a silent widening
// puts a field back on screen that the server no longer versions — with a
// restore button beside it promising something it cannot do.
//
// Note what is deliberately NOT asserted: an UNKNOWN field is appended rather
// than dropped (see the module header — a field the server grows before this
// file learns about it must still be readable). The invariants below are the
// two that ARE contracts: the declared order per kind, and the fields a ruling
// took OUT while the wire may still carry them.

import { describe, it, expect } from "vitest";
import {
  DOC_FIELD_ORDER,
  comparedFieldNames,
  documentFields,
} from "./docHistoryFields";

describe("DOC_FIELD_ORDER", () => {
  it("declares each kind exactly the fields that kind versions", () => {
    expect(DOC_FIELD_ORDER).toMatchObject({
      global_context: ["text"],
      // No `name`: the role's label is not versioned (owner 2026-07-31).
      role_definition: ["definition_md"],
      insight: ["text"],
      // The split manual series carries ONE field — that split is the whole
      // point of T-1f39, and a kind that listed the bundle's other fields
      // would re-bundle them on screen while the server versions only the SOP.
      task_manual_sop: ["sop_md"],
    });
  });
});

describe("documentFields", () => {
  it("hides a de-versioned field that an OLD revision still carries", () => {
    // Rows written before the 2026-07-31 ruling physically hold a name. The
    // append-unknown-fields rule would otherwise put it straight back on
    // screen, which is precisely what the ruling removed.
    const legacy = { name: "研究員", definition_md: "角色定義本文" };
    expect(documentFields("role_definition", legacy).map(([n]) => n)).toEqual([
      "definition_md",
    ]);
  });

  it("never treats the tombstone flag as content", () => {
    expect(documentFields("global_context", { tombstoned: "true" })).toEqual([]);
  });
});

describe("comparedFieldNames", () => {
  it("leaves a de-versioned field out of the comparison, from either side", () => {
    // A rename would otherwise surface as a difference that no restore can
    // undo — the server keeps the current name whatever the revision holds.
    expect(
      comparedFieldNames(
        "role_definition",
        { name: "舊名字", definition_md: "同一段本文" },
        { name: "新名字", definition_md: "同一段本文" }
      )
    ).toEqual(["definition_md"]);
  });
});
