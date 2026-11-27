import { zh, type Dict } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";

type BuiltinTypeNameLeaf = keyof Dict["tasks"]["builtinTypeName"];

const BUILTIN_TYPE_NAME_LEAF = new Map<string, BuiltinTypeNameLeaf>([
  ["builtin-role-design", "roleDesign"],
  ["builtin-task-manual-design", "taskManualDesign"],
  ["builtin-remote-access", "remoteAccess"],
]);

/** A task type's display label. The server now ships the built-in types under
 * their English names; stations seeded before that still store the old Chinese
 * ones. Either shipped name reads in the current language, so a zh UI shows the
 * Chinese label and an en UI the English one. Only while the stored name is
 * still a shipped one: a name the owner gave them shows as written.
 * Display only: an edit field must start from the stored displayName. */
export function taskTypeLabel(t: Dict, typeKey: string, displayName: string): string {
  const leaf = BUILTIN_TYPE_NAME_LEAF.get(typeKey);
  if (
    leaf !== undefined &&
    (displayName === zh.tasks.builtinTypeName[leaf] ||
      displayName === en.tasks.builtinTypeName[leaf])
  ) {
    return t.tasks.builtinTypeName[leaf];
  }
  return displayName || typeKey;
}
