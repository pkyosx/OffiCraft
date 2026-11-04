// Bundled into the pre-paint IIFE through themeDayparts.ts: no Vite-only syntax.

export interface Rgba {
  r: number;
  g: number;
  b: number;
  a: number;
}

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

function channel(tok: string): number | null {
  const pct = /^(-?[\d.]+)%$/.exec(tok);
  const n = Number(pct ? pct[1] : tok);
  if (!Number.isFinite(n)) return null;
  return clamp(pct ? (n / 100) * 255 : n, 0, 255);
}

function alpha(tok: string | undefined): number | null {
  if (tok === undefined) return 1;
  const pct = /^(-?[\d.]+)%$/.exec(tok);
  const n = Number(pct ? pct[1] : tok);
  if (!Number.isFinite(n)) return null;
  return clamp(pct ? n / 100 : n, 0, 1);
}

function hueDegrees(tok: string): number | null {
  const m = /^(-?[\d.]+)(deg|grad|rad|turn)?$/.exec(tok);
  if (!m) return null;
  const n = Number(m[1]);
  if (!Number.isFinite(n)) return null;
  const unit = m[2] ?? "deg";
  const deg = unit === "grad" ? n * 0.9 : unit === "rad" ? (n * 180) / Math.PI : unit === "turn" ? n * 360 : n;
  return ((deg % 360) + 360) % 360;
}

function percent(tok: string): number | null {
  const m = /^(-?[\d.]+)%?$/.exec(tok);
  if (!m) return null;
  const n = Number(m[1]);
  return Number.isFinite(n) ? clamp(n / 100, 0, 1) : null;
}

/** Splits `a, b, c, d` and `a b c / d` into at most four tokens. */
function functionArgs(body: string): string[] | null {
  const s = body.trim();
  const parts = s.includes(",")
    ? s.split(",").map((t) => t.trim())
    : s.split("/").flatMap((seg, i) => (i === 0 ? seg.trim().split(/\s+/) : [seg.trim()]));
  if (parts.length < 3 || parts.length > 4 || parts.some((t) => t === "")) return null;
  return parts;
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const k = (n: number) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  return [f(0) * 255, f(8) * 255, f(4) * 255];
}

/** Parses every form the theme colour grammar admits; null when it cannot. */
export function parseColor(value: string): Rgba | null {
  const v = value.trim().toLowerCase();
  if (v === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
  const hex = /^#([0-9a-f]{3,8})$/.exec(v);
  if (hex) {
    const h = hex[1];
    if (h.length === 5 || h.length === 7) return null;
    const short = h.length <= 4;
    const digits = short ? h.split("").map((c) => c + c) : h.match(/../g)!;
    const [r, g, b, a = "ff"] = digits.map((d) => d);
    return {
      r: parseInt(r, 16),
      g: parseInt(g, 16),
      b: parseInt(b, 16),
      a: parseInt(a, 16) / 255,
    };
  }
  const fn = /^(rgba?|hsla?)\((.*)\)$/.exec(v);
  if (!fn) return null;
  const args = functionArgs(fn[2]);
  if (!args) return null;
  const a = alpha(args[3]);
  if (a === null) return null;
  if (fn[1].startsWith("rgb")) {
    const [r, g, b] = args.slice(0, 3).map(channel);
    if (r === null || g === null || b === null) return null;
    return { r, g, b, a };
  }
  const h = hueDegrees(args[0]);
  const s = percent(args[1]);
  const l = percent(args[2]);
  if (h === null || s === null || l === null) return null;
  const [r, g, b] = hslToRgb(h, s, l);
  return { r, g, b, a };
}

export function formatColor(c: Rgba): string {
  const ch = (n: number) => Math.round(clamp(n, 0, 255));
  const a = Math.round(clamp(c.a, 0, 1) * 1000) / 1000;
  return `rgba(${ch(c.r)}, ${ch(c.g)}, ${ch(c.b)}, ${a})`;
}

/** Straight sRGB mix with t = 0 → from, t = 1 → to. */
export function mixColor(from: Rgba, to: Rgba, t: number): Rgba {
  const p = clamp(t, 0, 1);
  const lerp = (x: number, y: number) => x + (y - x) * p;
  return { r: lerp(from.r, to.r), g: lerp(from.g, to.g), b: lerp(from.b, to.b), a: lerp(from.a, to.a) };
}

/** WCAG relative luminance of the opaque colour (alpha ignored). */
export function relativeLuminance(c: Rgba): number {
  const lin = (n: number) => {
    const v = n / 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
}

/** Same threshold colorSchemeForBackground applies to `--color-bg`. */
export const DARK_LUMINANCE_BELOW = 0.2;

export function isDarkColor(c: Rgba): boolean {
  return relativeLuminance(c) < DARK_LUMINANCE_BELOW;
}

export function contrastRatio(x: Rgba, y: Rgba): number {
  const [hi, lo] = [relativeLuminance(x), relativeLuminance(y)].sort((p, q) => q - p);
  return (hi + 0.05) / (lo + 0.05);
}
