// themeTokenMeta.ts — T-16a1 P3b: the friendly-name + purpose-group overlay for
// the --color-* theme tokens. The theme editor never shows a raw `--color-*`
// name any more (owner: colour editing must be human-friendly); it shows a
// localized label and lays the tokens out by PURPOSE group.
//
// Deliberately NOT in the i18n locale dicts: those feed the wording whitelist
// (scripts/gen-message-keys.mjs extracts every en.ts string leaf), and a token
// label is editor chrome, not overridable UI copy — putting it there would let
// a theme "override the name of --color-accent", which is meaningless. So the
// labels carry their own {zh,en} here and are picked by the active language.

/** The purpose buckets a colour token can belong to (owner: 主色/背景/文字/
 * 狀態/裝飾…). Order below is the render order in the editor. */
export type TokenGroup =
  | "brand"
  | "background"
  | "text"
  | "status"
  | "presence"
  | "icon"
  | "dependency"
  | "decoration"
  | "other";

interface Label {
  zh: string;
  en: string;
}

interface TokenMeta {
  group: TokenGroup;
  label: Label;
}

export const GROUP_ORDER: TokenGroup[] = [
  "brand",
  "background",
  "text",
  "status",
  "presence",
  "icon",
  "dependency",
  "decoration",
  "other",
];

export const GROUP_LABEL: Record<TokenGroup, Label> = {
  brand: { zh: "主色", en: "Brand & accent" },
  background: { zh: "背景與表面", en: "Background & surface" },
  text: { zh: "文字", en: "Text" },
  status: { zh: "狀態", en: "Status" },
  presence: { zh: "狀態點", en: "Presence dots" },
  icon: { zh: "圖示", en: "Icons" },
  dependency: { zh: "相依", en: "Dependencies" },
  decoration: { zh: "裝飾", en: "Decoration" },
  other: { zh: "其他", en: "Other" },
};

// One entry per token in styles/themeTokens.generated.ts. A token absent here
// (e.g. a NEW token added to theme.css before this map catches up) degrades
// gracefully to the "other" group with its raw name — see tokenMeta().
const TOKEN_META: Record<string, TokenMeta> = {
  "--color-accent": { group: "brand", label: { zh: "主色", en: "Accent" } },
  "--color-accent-cta-bg": {
    group: "brand",
    label: { zh: "行動按鈕底", en: "CTA button" },
  },
  "--color-accent-cta-bg-hover": {
    group: "brand",
    label: { zh: "行動按鈕底(懸停)", en: "CTA button (hover)" },
  },
  "--color-accent-soft": {
    group: "brand",
    label: { zh: "主色(淡)", en: "Accent (soft)" },
  },
  "--color-indigo": { group: "brand", label: { zh: "靛藍", en: "Indigo" } },
  "--color-logo-grad-from": {
    group: "brand",
    label: { zh: "標誌漸層起", en: "Logo gradient from" },
  },
  "--color-logo-grad-to": {
    group: "brand",
    label: { zh: "標誌漸層迄", en: "Logo gradient to" },
  },
  "--color-select": { group: "brand", label: { zh: "選取", en: "Selection" } },
  "--color-switch-on": {
    group: "brand",
    label: { zh: "開關開啟", en: "Switch on" },
  },
  "--color-tab-active": {
    group: "brand",
    label: { zh: "分頁作用中", en: "Active tab" },
  },
  "--color-seg-border": {
    group: "brand",
    label: { zh: "分段邊框", en: "Segment border" },
  },
  "--color-seg-fill": {
    group: "brand",
    label: { zh: "分段填色", en: "Segment fill" },
  },
  "--color-step-progress": {
    group: "brand",
    label: { zh: "步驟進度", en: "Step progress" },
  },

  "--color-bg": {
    group: "background",
    label: { zh: "頁面背景", en: "Page background" },
  },
  "--color-card": {
    group: "background",
    label: { zh: "卡片背景", en: "Card background" },
  },
  "--color-border": {
    group: "background",
    label: { zh: "邊框", en: "Border" },
  },
  "--color-overlay": {
    group: "background",
    label: { zh: "浮層", en: "Overlay" },
  },
  "--color-scrim": {
    group: "background",
    label: { zh: "遮罩", en: "Scrim" },
  },
  "--color-shadow": {
    group: "background",
    label: { zh: "陰影", en: "Shadow" },
  },

  "--color-text": { group: "text", label: { zh: "內文", en: "Body text" } },
  "--color-text-muted": {
    group: "text",
    label: { zh: "次要文字", en: "Muted text" },
  },
  "--color-text-strong": {
    group: "text",
    label: { zh: "強調文字", en: "Strong text" },
  },
  "--color-on-accent": {
    group: "text",
    label: { zh: "主色上文字", en: "Text on accent" },
  },
  "--color-on-warn": {
    group: "text",
    label: { zh: "警告上文字", en: "Text on warning" },
  },
  "--color-task-id": {
    group: "text",
    label: { zh: "任務編號", en: "Task id" },
  },
  "--color-task-type": {
    group: "text",
    label: { zh: "任務類型", en: "Task type" },
  },

  "--color-success": { group: "status", label: { zh: "成功", en: "Success" } },
  "--color-danger": { group: "status", label: { zh: "危險", en: "Danger" } },
  "--color-danger-soft": {
    group: "status",
    label: { zh: "危險(淡)", en: "Danger (soft)" },
  },
  "--color-danger-strong": {
    group: "status",
    label: { zh: "危險(強)", en: "Danger (strong)" },
  },
  "--color-hot": { group: "status", label: { zh: "熱門", en: "Hot" } },
  "--color-answered": {
    group: "status",
    label: { zh: "已回覆", en: "Answered" },
  },
  "--color-status-duplicated": {
    group: "status",
    label: { zh: "重複狀態", en: "Duplicated" },
  },
  "--color-lock-reassigning": {
    group: "status",
    label: { zh: "重新指派鎖", en: "Reassigning lock" },
  },
  "--color-wait-external": {
    group: "status",
    label: { zh: "等待外部", en: "Waiting external" },
  },
  "--color-wait-external-text": {
    group: "status",
    label: { zh: "等待外部文字", en: "Waiting external text" },
  },
  "--color-warn-bg": {
    group: "status",
    label: { zh: "警告底", en: "Warning bg" },
  },
  "--color-warn-border": {
    group: "status",
    label: { zh: "警告邊框", en: "Warning border" },
  },
  "--color-warn-fg": {
    group: "status",
    label: { zh: "警告文字", en: "Warning text" },
  },
  "--color-callout-important": {
    group: "status",
    label: { zh: "提示:重要", en: "Callout: important" },
  },
  "--color-callout-note": {
    group: "status",
    label: { zh: "提示:註記", en: "Callout: note" },
  },
  "--color-callout-warning": {
    group: "status",
    label: { zh: "提示:警告", en: "Callout: warning" },
  },

  "--color-dot-offline": {
    group: "presence",
    label: { zh: "離線點", en: "Offline dot" },
  },
  "--color-dot-online": {
    group: "presence",
    label: { zh: "在線點", en: "Online dot" },
  },
  "--color-dot-stopped": {
    group: "presence",
    label: { zh: "已停止點", en: "Stopped dot" },
  },
  "--color-dot-stopping": {
    group: "presence",
    label: { zh: "停止中點", en: "Stopping dot" },
  },
  "--color-dot-waking": {
    group: "presence",
    label: { zh: "喚醒中點", en: "Waking dot" },
  },

  "--color-icon-amber": {
    group: "icon",
    label: { zh: "圖示:琥珀", en: "Icon: amber" },
  },
  "--color-icon-amber-text": {
    group: "icon",
    label: { zh: "圖示文字:琥珀", en: "Icon text: amber" },
  },
  "--color-icon-blue": {
    group: "icon",
    label: { zh: "圖示:藍", en: "Icon: blue" },
  },
  "--color-icon-blue-bg": {
    group: "icon",
    label: { zh: "圖示底:藍", en: "Icon bg: blue" },
  },
  "--color-icon-violet": {
    group: "icon",
    label: { zh: "圖示:紫", en: "Icon: violet" },
  },
  "--color-icon-violet-bg": {
    group: "icon",
    label: { zh: "圖示底:紫", en: "Icon bg: violet" },
  },

  "--color-dep": {
    group: "dependency",
    label: { zh: "相依", en: "Dependency" },
  },
  "--color-dep-missing": {
    group: "dependency",
    label: { zh: "相依缺失", en: "Dependency missing" },
  },
  "--color-dep-text": {
    group: "dependency",
    label: { zh: "相依文字", en: "Dependency text" },
  },
  "--color-dep-title": {
    group: "dependency",
    label: { zh: "相依標題", en: "Dependency title" },
  },

  "--color-artifacts": {
    group: "decoration",
    label: { zh: "產出物", en: "Artifacts" },
  },
  "--color-onboarding-bg": {
    group: "decoration",
    label: { zh: "導引底", en: "Onboarding bg" },
  },
  "--color-onboarding-border": {
    group: "decoration",
    label: { zh: "導引邊框", en: "Onboarding border" },
  },
  "--color-onboarding-fg": {
    group: "decoration",
    label: { zh: "導引文字", en: "Onboarding text" },
  },
  "--color-xian-avatar-glow": {
    group: "decoration",
    label: { zh: "修仙頭像光暈", en: "Xian avatar glow" },
  },
  "--color-xian-avatar-rim": {
    group: "decoration",
    label: { zh: "修仙頭像邊", en: "Xian avatar rim" },
  },
};

/** The friendly label + group for a token in the active language. An unmapped
 * token degrades to the raw token name in the "other" group so the editor never
 * hides a colour just because the map lags theme.css. */
export function tokenMeta(
  token: string,
  language: "zh" | "en"
): { group: TokenGroup; label: string } {
  const meta = TOKEN_META[token];
  if (!meta) return { group: "other", label: token };
  return { group: meta.group, label: meta.label[language] };
}

/** The localized name of a purpose group. */
export function groupLabel(group: TokenGroup, language: "zh" | "en"): string {
  return GROUP_LABEL[group][language];
}

/**
 * Convert a stored colour value to a `#rrggbb` the native `<input type=color>`
 * can seat its swatch on, or null when it has no lossless 6-digit-hex form (the
 * swatch then shows a neutral fallback while the exact value stays in the text
 * field). Accepts #rgb / #rgba / #rrggbb / #rrggbbaa — alpha is dropped for the
 * swatch only (the text field keeps the full value).
 */
export function toHex6(value: string): string | null {
  const v = value.trim().toLowerCase();
  const m = /^#([0-9a-f]{3,8})$/.exec(v);
  if (!m) return null;
  const h = m[1];
  if (h.length === 3 || h.length === 4) {
    return `#${h[0]}${h[0]}${h[1]}${h[1]}${h[2]}${h[2]}`;
  }
  if (h.length === 6 || h.length === 8) {
    return `#${h.slice(0, 6)}`;
  }
  return null;
}
