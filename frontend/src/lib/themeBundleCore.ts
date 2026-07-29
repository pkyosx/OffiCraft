// themeBundleCore.ts — T-16a1 P2 (split out of themeBundle.ts): the client-side theme-bundle validator, the
// character-for-character twin of the server grammar (server/ocserverd/
// theme_bundle.go). It is the SINGLE client implementation shared by the import
// UI (P2b), the theme picker (P2b), and the mock API's PATCH parity check — so
// a bundle rejected offline is rejected online for the identical reason.
//
// The security boundary is the colour VALUE. A bundle is applied via
// element.style.setProperty(name, value) — never concatenated into a stylesheet
// — but we still admit only CONCRETE colours through a strict allowlist grammar
// and constrain the token NAME to the generated theme.css whitelist.

import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import {
  THEME_FONT_TOKENS,
  SAFE_FONT_FAMILIES,
} from "../styles/themeFonts.generated";

/** One owner-authored theme colour bundle (mirrors ThemeBundleDTO). `wording`
 * is an OPTIONAL per-language message-key text-override overlay (T-16a1 P3).
 * `fonts` is an OPTIONAL --font-* → safe-family-stack overlay (T-16a1 P4). */
export interface ThemeBundle {
  id: string;
  name: string;
  colors: Record<string, string>;
  wording?: Record<string, Record<string, string>>;
  fonts?: Record<string, string>;
  /** Single-image identities kept for owner and assistant. */
  avatars?: Partial<Record<SingletonAvatarKind, string>>;
  /** Ordered staff/outsource pools. Actor identity is pool[avatarIndex % length]. */
  avatarPools?: Partial<Record<PoolAvatarKind, string[]>>;
  /** Optional studio logo image (T-ea81). A single EMBEDDED base64 `data:` URI
   * that replaces the built-in top-bar logo mark. Absent → the built-in mark. */
  logo?: string;
  /** Optional per-nav-tab icon images (T-ea81), keyed on the five nav tabs
   * (office / replies / tasks / monitor / guide). Each value is an EMBEDDED
   * base64 `data:` URI that replaces that tab's built-in icon. A tab the map
   * omits keeps its built-in icon (office never degrades). */
  navIcons?: Partial<Record<NavIconKey, string>>;
  /** Optional background images for the cockpit chrome (T-081b). Keyed on the
   * ZONE the image tiles across — a CLOSED set holding exactly `canvas`, the
   * OUTERMOST canvas beside the centred content column. Each value is an
   * EMBEDDED base64 `data:` URI laid on top of the solid `--color-bg`, which
   * still shows wherever the image does not. Absent → plain colour, exactly as
   * before this field existed. */
  backgrounds?: Partial<Record<BackgroundKey, string>>;
  /** Optional per-zone display mode for the `backgrounds` images (T-081b) —
   * `tile` (repeat over both axes; the behaviour that predates this field, and
   * the default for any zone omitted here), `sides` (one copy pinned against
   * each viewport edge) or `cover` (one copy scaled to fill the viewport).
   * Absent → every zone tiles, so an older bundle renders identically. */
  backgroundModes?: Partial<Record<BackgroundKey, BackgroundMode>>;
}

/** The roles an avatars overlay may key on (正職 / 外包 / owner / assistant) —
 * the character-for-character twin of the Go avatarKinds set (T-ea81). */
export const AVATAR_KINDS = ["member", "outsource", "owner", "assistant"] as const;
export type AvatarKind = (typeof AVATAR_KINDS)[number];
export const POOL_AVATAR_KINDS = ["member", "outsource"] as const;
export type PoolAvatarKind = (typeof POOL_AVATAR_KINDS)[number];
export const SINGLETON_AVATAR_KINDS = ["owner", "assistant"] as const;
export type SingletonAvatarKind = (typeof SINGLETON_AVATAR_KINDS)[number];
export const MAX_AVATAR_POOL_ITEMS = 12;

/** The nav tabs a navIcons overlay may key on — the closed set of the five main
 * nav tabs, identical to App.tsx's `Tab` type (T-ea81). */
export const NAV_ICON_KEYS = ["office", "replies", "tasks", "monitor", "guide"] as const;
export type NavIconKey = (typeof NAV_ICON_KEYS)[number];

/** The chrome zones a backgrounds overlay may key on — the twin of the Go
 * backgroundKeyAllowed set (T-081b). DELIBERATELY just the outermost canvas:
 * the topbar / nav / main zones sit under text, and text over a busy tiled
 * pattern has no readability guarantee, so they are not opened up. Keying on
 * the zone (rather than a bare `logo`-style string) is what makes that rule
 * structural — a bundle asking for `backgrounds.topbar` is rejected by name. */
export const BACKGROUND_KEYS = ["canvas"] as const;
export type BackgroundKey = (typeof BACKGROUND_KEYS)[number];

/** The display modes a background image may be laid down with — the twin of the
 * Go backgroundModeAllowed set (T-081b). `tile` repeats it over both axes (the
 * only behaviour that existed before this field, hence the default); `sides`
 * pins ONE copy against each viewport edge for art that reads as a pair of
 * standing objects rather than a texture (not mirrored — a theme wanting
 * symmetry bakes it into the image, owner 2026-07-27); `cover` scales one copy
 * to fill the whole viewport, for a single scene the cockpit floats on.
 *
 * ⚠️ `cover` is only VISIBLE where a theme also makes the chrome zones
 * (--color-topbar-bg / --color-nav-bg / --color-main-bg) translucent — the
 * colour grammar already admits #RRGGBBAA and rgba(), so that needs no new field.
 * That is also where its risk lives: those zones sit under text, and text over
 * a picture has no readability guarantee (owner accepted this on rc-f0e23286d75e). */
export const BACKGROUND_MODES = ["tile", "sides", "cover"] as const;
export type BackgroundMode = (typeof BACKGROUND_MODES)[number];
export const DEFAULT_BACKGROUND_MODE: BackgroundMode = "tile";

export const MAX_COLOR_VALUE_LEN = 64;
export const MIN_THEME_COLORS = 1;
export const MAX_THEME_COLORS = 200;
export const MAX_CUSTOM_THEMES = 100;
export const MAX_THEME_NAME_LEN = 80;

// Wording overlay bounds (T-16a1 P3) — the character-for-character twin of the
// Go constants in server/ocserverd/wording_bundle.go.
export const MAX_WORDING_VALUE_LEN = 200;
export const MAX_WORDING_ENTRIES_PER_LANG = 1000;

// Font overlay bound (T-16a1 P4) — the twin of maxFontValueLen in
// server/ocserverd/font_bundle.go. A font value is a whole family stack; the
// longest curated stack fits comfortably, so anything longer is only ever an
// injection attempt. Membership in the SAFE stack set is the real gate; this
// cap is a cheap pre-filter mirrored on the server.
export const MAX_FONT_VALUE_LEN = 128;

// Image bounds (T-16a1 P5; split per purpose in T-72da) — the twins of the Go
// constants in server/ocserverd/avatar_bundle.go. Each purpose gets a PAIR: the
// DECODED byte cap, and a raw data-URI string cap that sits above the
// base64-inflated (~4/3) decoded cap and is applied BEFORE decoding as a cheap
// pre-filter.
//
// The two pairs are deliberately different and must not be tidied back into
// one: an avatar / logo / nav icon is a 30–40 px glyph, where 64 KiB is already
// generous; a canvas background is stretched or tiled across the WHOLE viewport,
// where 64 KiB reads as visibly blurry (owner ruling 2026-08-03, overturning the
// T-081b decision that backgrounds must share the avatar cap — that decision's
// premise was that both went through ONE gate, which this split removes).
// Relaxing the wallpaper must NOT relax the glyph, so avatars stay at 64 KiB.
//
// Drift against the Go side is caught by bin/tests/fixtures/image-cap-cases.tsv
// (read by imageCap.test.ts here and image_cap_mirror_test.go there), not by
// this comment.
export const MAX_AVATAR_BYTES = 64 * 1024;
export const MAX_AVATAR_VALUE_LEN = 96 * 1024;
export const MAX_BACKGROUND_BYTES = 512 * 1024;
// 512 KiB decoded ≈ 682.7 KiB encoded; 704 KiB sits above that with margin. It
// MUST move with MAX_BACKGROUND_BYTES: it is checked before the decode, so
// leaving it at MAX_AVATAR_VALUE_LEN would reject every large background and
// the decoded cap would never be reached.
export const MAX_BACKGROUND_VALUE_LEN = 704 * 1024;

/** The closed RASTER mime whitelist an avatar image may declare. SVG
 * (image/svg+xml) is DELIBERATELY absent — it can carry <script>/onload (XSS). */
export const AVATAR_MIME_WHITELIST = [
  "image/png",
  "image/jpeg",
  "image/webp",
] as const;
const AVATAR_MIME_SET = new Set<string>(AVATAR_MIME_WHITELIST);

/** The built-in theme names a custom bundle must never claim. office is the
 * only built-in now (修仙 is an importable custom bundle, so "xian" is a
 * perfectly legal custom id). */
export const RESERVED_THEME_IDS = ["office"] as const;

/** The override languages a wording overlay may key on. */
export const WORDING_LANGS = ["zh", "en"] as const;

const THEME_TOKEN_SET = new Set<string>(THEME_COLOR_TOKENS);
// Font whitelists (T-16a1 P4): the --font-* token names and the CLOSED set of
// safe family-stack values — the character-for-character twin of the Go maps in
// theme_fonts_gen.go.
const THEME_FONT_TOKEN_SET = new Set<string>(THEME_FONT_TOKENS);
const SAFE_FONT_STACK_SET = new Set<string>(SAFE_FONT_FAMILIES.map((f) => f.stack));

// The colour-value allowlist grammar (anchored full-match) — identical to the
// Go regexps. Concrete colours only: hex / rgb(a) / hsl(a) / transparent.
const COLOR_HEX_RE = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/;
const COLOR_RGB_RE = /^rgba?\(\s*[0-9.,%/\s]+\)$/;
// hsl()/hsla(): rgb() char set plus the angle-unit letters {d,e,g,r,a,t,u,n}
// (deg/grad/rad/turn) on the hue — a closed set that cannot form url/var/
// expression/color-mix/javascript (all need a paren or colon this class forbids).
const COLOR_HSL_RE = /^hsla?\(\s*[0-9.,%/\sdegratun]+\)$/;
const THEME_ID_RE = /^[a-z0-9][a-z0-9-]{1,63}$/;

// Structure-breaking substrings a concrete colour can never legitimately hold.
// The grammar already rejects them; this yields a SPECIFIC "you pasted CSS" error.
const COLOR_INJECTION_MARKERS = [
  "url(", "expression(", "var(", "color-mix(", "image(", "element(",
  "javascript:", "/*", "*/", ";", "{", "}", "<", ">", "@", "\\", "`", "\n", "\r",
];

/** Whether v is an admissible concrete colour value. */
export function isValidColorValue(v: string): boolean {
  if (v === "" || v.length > MAX_COLOR_VALUE_LEN) return false;
  for (const m of COLOR_INJECTION_MARKERS) {
    if (v.includes(m)) return false;
  }
  if (v === "transparent") return true;
  return COLOR_HEX_RE.test(v) || COLOR_RGB_RE.test(v) || COLOR_HSL_RE.test(v);
}

// Structure-breaking substrings a font-family stack can never legitimately
// hold — the same defence-in-depth pass colours get. A font value is admitted
// only by EXACT membership in SAFE_FONT_STACK_SET (a closed allowlist), so a
// value that reaches setProperty is already safe; this second pass exists so a
// smuggled value (`url(`, `@font-face`, `;}`, `javascript:`, …) is rejected with
// a SPECIFIC error before the membership check, mirroring colorInjectionMarkers.
const FONT_INJECTION_MARKERS = [
  "url(", "expression(", "var(", "javascript:", "/*", "*/",
  ";", "{", "}", "<", ">", "@", "\\", "`", "\n", "\r", "(",
];

/** Whether v is an admissible font value: a member of the CLOSED safe-family
 * stack allowlist, with no CSS-structure characters (defence in depth). */
export function isValidFontValue(v: string): boolean {
  if (v === "" || v.length > MAX_FONT_VALUE_LEN) return false;
  for (const m of FONT_INJECTION_MARKERS) {
    if (v.includes(m)) return false;
  }
  return SAFE_FONT_STACK_SET.has(v);
}

/** Validate a bundle's optional `fonts` overlay (T-16a1 P4) — the twin of the
 * Go validateFonts. Key ∈ {--font-* token whitelist}, value ∈ {safe family
 * stack allowlist}. Returns an error message, or null when admissible (an
 * absent overlay is admissible). */
export function validateFonts(fonts: unknown, where = "theme"): string | null {
  if (fonts === undefined || fonts === null) return null;
  if (typeof fonts !== "object" || Array.isArray(fonts)) {
    return `${where}: fonts must be an object`;
  }
  for (const [token, value] of Object.entries(fonts as Record<string, unknown>)) {
    if (!THEME_FONT_TOKEN_SET.has(token)) {
      return `${where}: "${token}" is not a theme font token (only --font-sans / --font-title)`;
    }
    if (typeof value !== "string" || !isValidFontValue(value)) {
      return `${where}: "${token}" has an invalid font value — only a safe built-in font family may be chosen`;
    }
  }
  return null;
}

const NAV_ICON_KEY_SET = new Set<string>(NAV_ICON_KEYS);
const BACKGROUND_KEY_SET = new Set<string>(BACKGROUND_KEYS);
const BACKGROUND_MODE_SET = new Set<string>(BACKGROUND_MODES);

// Magic-byte predicates over the DECODED image bytes — the twin of
// avatarMimeMagic in avatar_bundle.go. Each whitelisted mime must begin with
// its format's signature, so a value that declares one mime but carries another
// (e.g. a script-bearing SVG behind a png claim) is rejected.
const AVATAR_MIME_MAGIC: Record<string, (b: Uint8Array) => boolean> = {
  // PNG: 89 50 4E 47 0D 0A 1A 0A
  "image/png": (b) =>
    b.length >= 8 &&
    b[0] === 0x89 && b[1] === 0x50 && b[2] === 0x4e && b[3] === 0x47 &&
    b[4] === 0x0d && b[5] === 0x0a && b[6] === 0x1a && b[7] === 0x0a,
  // JPEG: FF D8 FF
  "image/jpeg": (b) => b.length >= 3 && b[0] === 0xff && b[1] === 0xd8 && b[2] === 0xff,
  // WEBP: "RIFF" .... "WEBP"
  "image/webp": (b) =>
    b.length >= 12 &&
    b[0] === 0x52 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x46 &&
    b[8] === 0x57 && b[9] === 0x45 && b[10] === 0x42 && b[11] === 0x50,
};

/** Decode a strict-standard base64 string to bytes, or null when it is not
 * valid base64. Uses atob (browser + modern Node/jsdom) then re-encodes to
 * confirm the round-trip (atob is lenient; a strict compare rejects stray
 * characters / bad padding the Go decoder would also reject). */
function decodeBase64(s: string): Uint8Array | null {
  // Strict standard base64: A–Z a–z 0–9 + / with mandatory padding to a
  // multiple of 4. Reject anything else BEFORE atob (which is lenient).
  if (s.length === 0 || s.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(s)) {
    return null;
  }
  try {
    const bin = atob(s);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return bytes;
  } catch {
    return null;
  }
}

/** Whether v is an admissible embedded image at the given size caps: a
 * `data:image/<whitelisted mime>;base64,<base64>` URI that decodes within
 * maxDecoded bytes, whose raw string is at most maxValueLen bytes, and whose
 * magic bytes match the declared mime. The twin of validImageValue in
 * avatar_bundle.go (the security boundary — this is a new attack surface, an
 * image the browser renders). Everything but the two caps is shared by every
 * purpose; only "how big" is per-purpose (T-72da). */
export function isValidImageValue(
  v: string,
  maxDecoded: number,
  maxValueLen: number
): boolean {
  if (v === "" || v.length > maxValueLen) return false;
  if (!v.startsWith("data:")) return false;
  const comma = v.indexOf(",");
  if (comma < 0) return false;
  const meta = v.slice("data:".length, comma); // "image/png;base64"
  const payload = v.slice(comma + 1);
  if (!meta.endsWith(";base64")) return false;
  const mime = meta.slice(0, -";base64".length);
  if (!AVATAR_MIME_SET.has(mime)) return false;
  const bytes = decodeBase64(payload);
  if (bytes === null || bytes.length === 0 || bytes.length > maxDecoded) {
    return false;
  }
  return AVATAR_MIME_MAGIC[mime]?.(bytes) ?? false;
}

/** The image gate at the AVATAR caps (64 KiB) — for avatars, logo and navIcons,
 * i.e. the small glyphs. The twin of validAvatarValue in avatar_bundle.go. */
export function isValidAvatarValue(v: string): boolean {
  return isValidImageValue(v, MAX_AVATAR_BYTES, MAX_AVATAR_VALUE_LEN);
}

/** The same image gate at the BACKGROUND caps (512 KiB) — only `backgrounds`
 * values come through here. The twin of validBackgroundValue in
 * avatar_bundle.go. See the const block for why a full-viewport image is
 * allowed more room than a 30–40 px glyph. */
export function isValidBackgroundValue(v: string): boolean {
  return isValidImageValue(v, MAX_BACKGROUND_BYTES, MAX_BACKGROUND_VALUE_LEN);
}

/** Validate a bundle's optional `avatars` overlay (T-16a1 P5; T-ea81) — the
 * twin of the Go validateAvatars. Key ∈ {member, outsource, owner, assistant},
 * value ∈ {whitelisted-raster base64 data URI}. Returns an error message, or
 * null when admissible (an absent overlay is admissible). */
export function validateAvatars(avatars: unknown, where = "theme"): string | null {
  if (avatars === undefined || avatars === null) return null;
  if (typeof avatars !== "object" || Array.isArray(avatars)) {
    return `${where}: avatars must be an object`;
  }
  for (const [kind, value] of Object.entries(avatars as Record<string, unknown>)) {
    if (!(SINGLETON_AVATAR_KINDS as readonly string[]).includes(kind)) {
      return `${where}: avatar kind "${kind}" is not allowed (only owner, assistant)`;
    }
    if (typeof value !== "string" || !isValidAvatarValue(value)) {
      return `${where}: avatars[${kind}] is not a valid image — only a base64 data: URI of a PNG / JPEG / WEBP (≤ 64 KiB) is accepted`;
    }
  }
  return null;
}

export function normalizeThemeAvatarPools(
  bundle: Record<string, unknown>,
  where = "theme"
): string | null {
  if (
    bundle.avatars === undefined ||
    bundle.avatars === null ||
    typeof bundle.avatars !== "object" ||
    Array.isArray(bundle.avatars)
  ) {
    return null;
  }
  const avatars = bundle.avatars as Record<string, unknown>;
  let pools =
    bundle.avatarPools &&
    typeof bundle.avatarPools === "object" &&
    !Array.isArray(bundle.avatarPools)
      ? (bundle.avatarPools as Record<string, unknown>)
      : undefined;
  for (const kind of POOL_AVATAR_KINDS) {
    if (!(kind in avatars)) continue;
    if (pools && kind in pools) {
      return `${where}: cannot define both avatars[${kind}] and avatarPools[${kind}]`;
    }
    pools ??= {};
    pools[kind] = [avatars[kind]];
    delete avatars[kind];
  }
  if (pools) bundle.avatarPools = pools;
  if (Object.keys(avatars).length === 0) delete bundle.avatars;
  return null;
}

export function validateAvatarPools(
  pools: unknown,
  where = "theme"
): string | null {
  if (pools === undefined || pools === null) return null;
  if (typeof pools !== "object" || Array.isArray(pools)) {
    return `${where}: avatarPools must be an object`;
  }
  for (const [kind, values] of Object.entries(pools as Record<string, unknown>)) {
    if (!(POOL_AVATAR_KINDS as readonly string[]).includes(kind)) {
      return `${where}: avatar pool kind "${kind}" is not allowed (only member, outsource)`;
    }
    if (!Array.isArray(values) || values.length > MAX_AVATAR_POOL_ITEMS) {
      return `${where}: avatarPools[${kind}] must be an array with at most ${MAX_AVATAR_POOL_ITEMS} images`;
    }
    for (let i = 0; i < values.length; i++) {
      if (typeof values[i] !== "string" || !isValidAvatarValue(values[i])) {
        return `${where}: avatarPools[${kind}][${i}] is not a valid image — only a base64 data: URI of a PNG / JPEG / WEBP (≤ 64 KiB) is accepted`;
      }
    }
  }
  return null;
}

/** Validate a bundle's optional `logo` image (T-ea81) — the twin of the Go
 * validateLogo. Reuses the SAME image gate as avatars (isValidAvatarValue): a
 * whitelisted-raster base64 data URI. Returns an error message, or null when
 * admissible (an absent logo is admissible). */
export function validateLogo(logo: unknown, where = "theme"): string | null {
  if (logo === undefined || logo === null) return null;
  if (typeof logo !== "string" || !isValidAvatarValue(logo)) {
    return `${where}: logo is not a valid image — only a base64 data: URI of a PNG / JPEG / WEBP (≤ 64 KiB) is accepted`;
  }
  return null;
}

/** Validate a bundle's optional `navIcons` overlay (T-ea81) — the twin of the
 * Go validateNavIcons. Key ∈ {office, replies, tasks, monitor, guide}, value ∈
 * {whitelisted-raster base64 data URI, via the SAME gate as avatars}. Returns an
 * error message, or null when admissible (an absent overlay is admissible). */
export function validateNavIcons(navIcons: unknown, where = "theme"): string | null {
  if (navIcons === undefined || navIcons === null) return null;
  if (typeof navIcons !== "object" || Array.isArray(navIcons)) {
    return `${where}: navIcons must be an object`;
  }
  for (const [key, value] of Object.entries(navIcons as Record<string, unknown>)) {
    if (!NAV_ICON_KEY_SET.has(key)) {
      return `${where}: nav icon key "${key}" is not allowed (only office, replies, tasks, monitor, guide)`;
    }
    if (typeof value !== "string" || !isValidAvatarValue(value)) {
      return `${where}: navIcons[${key}] is not a valid image — only a base64 data: URI of a PNG / JPEG / WEBP (≤ 64 KiB) is accepted`;
    }
  }
  return null;
}

/** Validate a bundle's optional `backgrounds` overlay (T-081b) — the twin of the
 * Go validateBackgrounds. Key ∈ {canvas}, value ∈ {whitelisted-raster base64
 * data URI via isValidBackgroundValue: the same mime allowlist, SVG refusal and
 * magic-byte check as an avatar — those are the security boundary and are shared
 * — but at the 512 KiB decoded cap rather than the avatar's 64 KiB.
 *
 * T-081b ruled the opposite ("the cap is NOT raised for backgrounds"). The owner
 * overturned that on 2026-08-03: a canvas background covers the whole viewport
 * and at 64 KiB his real background read as blurry. That ruling's premise — one
 * shared gate, so relaxing backgrounds would relax avatars too — no longer
 * holds: the caps are parameters now, and avatars / logo / navIcons keep 64 KiB.
 *
 * Returns an error message, or null when admissible (an absent overlay is
 * admissible). */
export function validateBackgrounds(backgrounds: unknown, where = "theme"): string | null {
  if (backgrounds === undefined || backgrounds === null) return null;
  if (typeof backgrounds !== "object" || Array.isArray(backgrounds)) {
    return `${where}: backgrounds must be an object`;
  }
  for (const [key, value] of Object.entries(backgrounds as Record<string, unknown>)) {
    if (!BACKGROUND_KEY_SET.has(key)) {
      return `${where}: background zone "${key}" is not allowed (only canvas)`;
    }
    if (typeof value !== "string" || !isValidBackgroundValue(value)) {
      return `${where}: backgrounds[${key}] is not a valid image — only a base64 data: URI of a PNG / JPEG / WEBP (≤ 512 KiB) is accepted`;
    }
  }
  return null;
}

/** Validate a bundle's optional `backgroundModes` map (T-081b) — the twin of the
 * Go validateBackgroundModes. Key ∈ the SAME zone set as backgrounds and must
 * carry an image there (a mode on an imageless zone paints nothing, so it is a
 * mistake worth naming rather than ignoring); value ∈ {tile, sides}. An absent
 * map means every zone tiles, which is what the field's absence has always
 * meant. */
export function validateBackgroundModes(
  modes: unknown,
  backgrounds: unknown,
  where = "theme"
): string | null {
  if (modes === undefined || modes === null) return null;
  if (typeof modes !== "object" || Array.isArray(modes)) {
    return `${where}: backgroundModes must be an object`;
  }
  const images = (backgrounds ?? {}) as Record<string, unknown>;
  for (const [key, value] of Object.entries(modes as Record<string, unknown>)) {
    if (!BACKGROUND_KEY_SET.has(key)) {
      return `${where}: background zone "${key}" is not allowed (only canvas)`;
    }
    if (typeof images[key] !== "string" || images[key] === "") {
      return `${where}: backgroundModes[${key}] has no image in backgrounds[${key}]`;
    }
    if (typeof value !== "string" || !BACKGROUND_MODE_SET.has(value)) {
      return `${where}: backgroundModes[${key}] is not a valid mode (only tile, sides, cover)`;
    }
  }
  return null;
}

export function runeCount(s: string): number {
  return [...s].length;
}

/** Whether a rune is a control character (Unicode Cc: 0x00-0x1F, 0x7F-0x9F) —
 * mirrors Go's unicode.IsControl, used to reject newlines / control chars in a
 * wording value. */
export function hasControlChar(s: string): boolean {
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0;
    if (cp <= 0x1f || (cp >= 0x7f && cp <= 0x9f)) return true;
  }
  return false;
}

/** The character CLASSES a theme display name may not carry, expressed as
 * Unicode general categories rather than as a hand-listed set of codepoints
 * (T-081b review round 4, SHOULD-C). Round 3 listed six zero-width codepoints
 * and every unlisted member of the SAME categories walked straight through:
 * U+00AD SOFT HYPHEN and U+180E and the U+E00xx TAG block (all Cf, like the
 * ZWSP that WAS listed), U+2028/U+2029 (Zl/Zp), U+00A0 / U+1680 / U+3000 (Zs).
 * Each of them is invisible in the rendered name, so two names that look
 * identical on screen can differ in bytes — which is how bad data gets in.
 *
 *   Cc control · Cf format (bidi marks, ZWSP/ZWNJ/ZWJ, WORD JOINER, BOM,
 *   SOFT HYPHEN, the TAG block) · Co private use · Cs surrogate ·
 *   Zl line separator · Zp paragraph separator
 *
 * Zs (space separator) is NOT here — it is NORMALISED to U+0020 instead, see
 * normalizeThemeSpaces (T-081b review round 4 recheck, SHOULD-3). Rejecting it
 * blocked 「深海　之夜」, which is simply what a Chinese IME in full-width mode
 * emits when the user presses the space bar, and told them so in a message
 * written for implementers. Normalising loses nothing on the security side —
 * 「　辦公室　」 still fails, now against the reserved-name rule that names the
 * actual reason — and keeps the legitimate names.
 *
 * DELIBERATELY NOT REJECTED: the variation selectors (U+FE0F & co., category
 * Mn). They are zero-width, but they are also how a legitimate emoji name is
 * spelled (「Heart ❤️」), and Mn holds every combining accent besides — banning
 * the category would reject ordinary Vietnamese, Hebrew-with-points and
 * Devanagari names. The accepted cost is documented in round4-fix-report.md.
 *
 * The `u` flag + property escapes make this the SAME table the engine ships,
 * not a third hand-kept list; the Go twin (hasInvisibleNameRune in
 * server/ocserverd/theme_bundle.go) reads the same categories out of the
 * standard library's `unicode` package, and themeName.parity.test.ts feeds both ends
 * the same 61 names and fails on ANY divergence — including one caused by the
 * two runtimes shipping different Unicode versions. */
const INVISIBLE_NAME_CLASS_RE = /[\p{Cc}\p{Cf}\p{Co}\p{Cs}\p{Zl}\p{Zp}]/u;

/** Whether s carries a character from one of the rejected classes. The twin of
 * the Go hasInvisibleNameRune. */
function hasInvisibleNameRune(s: string): boolean {
  for (const ch of s) {
    if (INVISIBLE_NAME_CLASS_RE.test(ch)) return true;
  }
  return false;
}

/** Every space separator folded onto U+0020 — the FIRST thing done to a name,
 * before it is trimmed, measured or compared against a built-in's.
 *
 * U+3000 IDEOGRAPHIC SPACE, U+00A0 NO-BREAK SPACE and the rest of Zs are
 * ordinary spaces as far as a human reading the name is concerned, so treating
 * them as one is what makes both halves come out right: 「深海　之夜」 is accepted
 * and stored as an ordinary two-word name, while 「　辦公室　」 collapses onto
 * 「辦公室」 and is refused BY THE RESERVED-NAME RULE, which can say so.
 *
 * The twin of the Go normalizeThemeSpaces. */
const SPACE_SEPARATOR_RE = /\p{Zs}/gu;

function normalizeThemeSpaces(s: string): string {
  return s.replace(SPACE_SEPARATOR_RE, " ");
}

// The edge trim is ASCII-only — deliberately not `.trim()`, whose ECMAScript
// WhiteSpace set differs from Go's unicode.IsSpace (U+FEFF, U+0085), which is
// how the two validators came to decide name length — and name identity — on
// different strings. An explicit ASCII set is identical BY CONSTRUCTION on both
// sides, and it stays EXHAUSTIVE because normalizeThemeSpaces runs first: every
// non-ASCII space has already become U+0020, and every non-ASCII whitespace that
// is NOT a space (Zl/Zp, U+0085, U+FEFF) is rejected outright. Nothing is left
// for the two languages to disagree about. The twin of the Go trimThemeName.
const ASCII_SPACE_EDGES_RE = /^[\t\n\v\f\r ]+|[\t\n\v\f\r ]+$/g;

/** EXPORTED for its own unit test, not for call sites: it decides the LENGTH
 * verdict on both ends of the wire, and nothing observable through
 * validateThemeBundle can tell `.trim()` from the explicit ASCII set (they
 * differ only on U+0085 / U+FEFF, which hasInvisibleNameRune rejects first). So
 * the two sides' agreement is pinned on the normaliser itself, against the twin
 * table in server/ocserverd/theme_bundle_test.go. */
export function trimThemeName(s: string): string {
  return normalizeThemeSpaces(s).replace(ASCII_SPACE_EDGES_RE, "");
}

/** Validate one bundle. Returns an error message, or null when admissible.
 * `skipped` is the same warning channel validateWording documents: unrecognised
 * wording codes are dropped and named here, never turned into an error. */
export type WordingValidator = (
  wording: unknown,
  where?: string,
  skipped?: string[]
) => string | null;

export function validateThemeBundleWith(
  b: unknown,
  where: string,
  skipped: string[] | undefined,
  validateWordingFn: WordingValidator | null
): string | null {
  if (typeof b !== "object" || b === null) {
    return `${where}: must be an object`;
  }
  const bundle = b as Record<string, unknown>;
  const normalizeErr = normalizeThemeAvatarPools(bundle, where);
  if (normalizeErr) return normalizeErr;
  if (typeof bundle.id !== "string" || !THEME_ID_RE.test(bundle.id)) {
    return `${where}: id must match ^[a-z0-9][a-z0-9-]{1,63}$`;
  }
  if ((RESERVED_THEME_IDS as readonly string[]).includes(bundle.id)) {
    return `${where}: id "${bundle.id}" is reserved for a built-in theme`;
  }
  if (typeof bundle.name !== "string") {
    return `${where}: name must be a string`;
  }
  const n = runeCount(trimThemeName(bundle.name));
  if (n < 1 || n > MAX_THEME_NAME_LEN) {
    return `${where}: name must be 1..${MAX_THEME_NAME_LEN} characters after trimming`;
  }
  if (hasInvisibleNameRune(bundle.name)) {
    return `${where}: name must not contain control, formatting, private-use, surrogate or line/paragraph separator characters`;
  }
  if (
    typeof bundle.colors !== "object" ||
    bundle.colors === null ||
    Array.isArray(bundle.colors)
  ) {
    return `${where}: colors must be an object`;
  }
  const colors = bundle.colors as Record<string, unknown>;
  const entries = Object.entries(colors);
  if (entries.length < MIN_THEME_COLORS || entries.length > MAX_THEME_COLORS) {
    return `${where}: colors must hold ${MIN_THEME_COLORS}..${MAX_THEME_COLORS} entries`;
  }
  for (const [token, value] of entries) {
    if (!THEME_TOKEN_SET.has(token)) {
      return `${where}: "${token}" is not a theme colour token (see theme.css)`;
    }
    if (typeof value !== "string" || !isValidColorValue(value)) {
      return `${where}: "${token}" has an invalid colour value — only concrete hex / rgb() / rgba() / hsl() / hsla() / transparent are accepted`;
    }
  }
  // wording (T-16a1 P3) is an optional per-language text-override overlay.
  if (validateWordingFn === null) {
    // The paint path (lib/themePaint.ts) has no wording sink at all, so the
    // wording grammar — and the 25 kB message-key whitelist behind it — is not
    // in its import graph. A record that carries `wording` anyway is NOT
    // silently accepted: it is rejected, so "this path never sees wording" is
    // an ENFORCED invariant rather than an assumption.
    if ((bundle as { wording?: unknown }).wording !== undefined) {
      return `${where}: wording is not accepted on this path`;
    }
  } else {
    const wErr = validateWordingFn(
      (bundle as { wording?: unknown }).wording,
      where,
      skipped
    );
    if (wErr) return wErr;
  }
  // fonts (T-16a1 P4) is an optional --font-* → safe-family overlay.
  const fErr = validateFonts((bundle as { fonts?: unknown }).fonts, where);
  if (fErr) return fErr;
  // avatars (T-16a1 P5; T-ea81) is an optional per-role embedded-image overlay.
  const aErr = validateAvatars((bundle as { avatars?: unknown }).avatars, where);
  if (aErr) return aErr;
  const apErr = validateAvatarPools(
    (bundle as { avatarPools?: unknown }).avatarPools,
    where
  );
  if (apErr) return apErr;
  // logo (T-ea81) is an optional single embedded studio-logo image.
  const lErr = validateLogo((bundle as { logo?: unknown }).logo, where);
  if (lErr) return lErr;
  // navIcons (T-ea81) is an optional per-nav-tab embedded-image overlay.
  const nErr = validateNavIcons((bundle as { navIcons?: unknown }).navIcons, where);
  if (nErr) return nErr;
  // backgrounds (T-081b) is an optional outer-canvas image overlay.
  const bgErr = validateBackgrounds(
    (bundle as { backgrounds?: unknown }).backgrounds,
    where
  );
  if (bgErr) return bgErr;
  // backgroundModes (T-081b) says HOW each of those images is laid down.
  return validateBackgroundModes(
    (bundle as { backgroundModes?: unknown }).backgroundModes,
    (bundle as { backgrounds?: unknown }).backgrounds,
    where
  );
}

/** Validate the whole custom_themes array (shape + token whitelist + colour
 * grammar + unique ids). Returns an error message, or null when admissible. */
export function validateThemeBundlesWith(
  bundles: unknown,
  validateOne: (b: unknown, where: string) => string | null
): string | null {
  if (!Array.isArray(bundles)) {
    return "custom_themes must be an array";
  }
  if (bundles.length > MAX_CUSTOM_THEMES) {
    return `custom_themes must hold at most ${MAX_CUSTOM_THEMES} themes`;
  }
  const seen = new Set<string>();
  for (let i = 0; i < bundles.length; i++) {
    const err = validateOne(bundles[i], `custom_themes[${i}]`);
    if (err) return err;
    const id = (bundles[i] as ThemeBundle).id;
    if (seen.has(id)) return `custom_themes[${i}]: duplicate id "${id}"`;
    seen.add(id);
  }
  return null;
}

/** Whether a display.theme value is admissible given the custom id set:
 * "" (unset) | a built-in | an existing custom id. */
export function isValidDisplayTheme(theme: string, customIds: Set<string>): boolean {
  return (
    theme === "" ||
    (RESERVED_THEME_IDS as readonly string[]).includes(theme) ||
    customIds.has(theme)
  );
}
