import { describe, it, expect } from "vitest";
import { taskTypeLabel } from "./taskTypeLabel";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

describe("taskTypeLabel", () => {
  it("reads a built-in type under its shipped name in the current language", () => {
    expect(taskTypeLabel(en, "builtin-task-manual-design", "建立／修改任務手冊")).toBe(
      "Create / edit task manual",
    );
    expect(taskTypeLabel(en, "builtin-role-design", "建立／修改角色")).toBe("Create / edit role");
    expect(taskTypeLabel(zh, "builtin-role-design", "建立／修改角色")).toBe("建立／修改角色");
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
