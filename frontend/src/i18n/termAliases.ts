// The OffiCraft feature names a member's boot context maps between the Chinese
// screen and its English documents (the zh User Language block). Each concept
// names dictionary KEYS, never strings: scripts/gen-term-aliases.mjs resolves
// them in zh.ts and en.ts and writes server/ocserverd/term_aliases_gen.go and
// spec/term-aliases.json. Regenerate with `npm run gen:termaliases`.
//
// A name is a key whose string is the label itself, or — for a concept the
// screen only shows inside a longer label — the name plus the key whose zh and
// en strings contain it (`foundIn`); the generator fails if either does not.

export type TermName = string | { zh: string; en: string; foundIn: string };

export const TERM_ALIASES: readonly (readonly TermName[])[] = [
  ["nav.office"],
  [{ zh: "請示卡", en: "Reply card", foundIn: "settings.suggestedRepliesReplyCardSub" }, "nav.replies"],
  ["nav.tasks"],
  ["tasks.typeLabel"],
  ["tasks.progressLabel"],
  ["settings.manuals"],
  ["nav.sharedDocs"],
  ["nav.secrets"],
  ["nav.lore"],
  [{ zh: "成員傳承", en: "Member lore", foundIn: "settings.loreCapRole" }],
  [{ zh: "任務傳承", en: "Task lore", foundIn: "settings.loreCapManualSub" }],
  ["lore.loreTypeInstructionConflict"],
  ["lore.loreTypeInstructionSupplement"],
  ["lore.loreTypeOwnerDecision"],
  ["lore.loreTypeOwnerPreference"],
  ["nav.monitor"],
  ["settings.title"],
  ["settings.globalContext"],
  ["settings.systemName"],
  ["settings.customName"],
  ["settings.bootName"],
  ["settings.roles"],
  ["mp.resumeSummary.rosterDutyLabel"],
  [{ zh: "判準", en: "Insight", foundIn: "mp.insight" }],
  ["settings.params"],
  ["settings.software"],
  ["profile.theme"],
  ["profile.preferences"],
  ["settings.assigneeKindMember"],
  ["office.staffTitle"],
  ["office.outsource.title"],
  ["office.role.assistant"],
  ["lifecycle.action.spawn"],
  ["mp.refocus"],
  ["lifecycle.action.stop"],
  ["lifecycle.action.accelerated-stop"],
  ["mp.resumeSummary.machinesSection"],
  [{ zh: "回呼端點", en: "Webhook endpoints", foundIn: "mp.webhook.empty" }],
  ["signingKeys.title"],
  ["guide.title"],
];
