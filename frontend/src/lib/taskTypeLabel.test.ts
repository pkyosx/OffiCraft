import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { taskTypeLabel } from "./taskTypeLabel";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

const ASSETS_GO = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "server",
  "ocserverd",
  "assets.go",
);

describe("taskTypeLabel", () => {
  it("reads a built-in type under its shipped name in the current language", () => {
    expect(taskTypeLabel(en, "builtin-task-manual-design", "建立／修改任務手冊")).toBe(
      "Create / edit task manual",
    );
    expect(taskTypeLabel(en, "builtin-role-design", "建立／修改角色")).toBe("Create / edit role");
    expect(taskTypeLabel(zh, "builtin-role-design", "建立／修改角色")).toBe("建立／修改角色");
  });

  it("translates every built-in type under the exact name the server ships, read out of assets.go", () => {
    const src = readFileSync(ASSETS_GO, "utf8");
    const shipped = [
      ...src.matchAll(/TypeKey:\s*"(builtin-[^"]+)",\s*\n\s*DisplayName:\s*"([^"]+)"/g),
    ].map((m) => [m[1], m[2]]);
    expect(
      shipped.map(([key]) => key),
      "server/ocserverd/assets.go no longer lists the built-in manuals as TypeKey/DisplayName " +
        "pairs this guard can read — point it at their new home.",
    ).toEqual(["builtin-role-design", "builtin-task-manual-design"]);
    expect(
      shipped.map(([key, name]) => taskTypeLabel(en, key, name)),
    ).toEqual([en.tasks.builtinTypeName.roleDesign, en.tasks.builtinTypeName.taskManualDesign]);
  });

  it("shows a renamed built-in type as stored", () => {
    expect(taskTypeLabel(en, "builtin-role-design", "Hire a role")).toBe("Hire a role");
  });

  it("falls back to the type key when the name is empty", () => {
    expect(taskTypeLabel(en, "builtin-role-design", "")).toBe("builtin-role-design");
    expect(taskTypeLabel(en, "tm-review", "")).toBe("tm-review");
  });

  it("shows an ordinary type's display name, even one spelled like a built-in's", () => {
    expect(taskTypeLabel(en, "tm-review", "PR 審查")).toBe("PR 審查");
    expect(taskTypeLabel(en, "tm-copy", "建立／修改角色")).toBe("建立／修改角色");
  });
});
