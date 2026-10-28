import { zh, type Dict } from "../i18n/locales/zh";

type BuiltinTypeNameLeaf = keyof Dict["tasks"]["builtinTypeName"];

const BUILTIN_TYPE_NAME_LEAF = new Map<string, BuiltinTypeNameLeaf>([
  ["builtin-role-design", "roleDesign"],
  ["builtin-task-manual-design", "taskManualDesign"],
]);

/** A task type's display label. The server stores the built-in types' names in
 * Chinese; those read in the current language only while the stored name is
 * still the shipped one, so a name the owner gave them shows as written.
 * Display only: an edit field must start from the stored displayName. */
export function taskTypeLabel(t: Dict, typeKey: string, displayName: string): string {
  const leaf = BUILTIN_TYPE_NAME_LEAF.get(typeKey);
  if (leaf !== undefined && displayName === zh.tasks.builtinTypeName[leaf]) {
    return t.tasks.builtinTypeName[leaf];
  }
  return displayName || typeKey;
}
