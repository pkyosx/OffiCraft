// Bundled into the pre-paint IIFE: no Vite-only syntax.
import { formatColor, isDarkColor, mixColor, parseColor } from "./themeColor";
import type { BackgroundKey, BackgroundMode, ThemeBundle, ThemeDaypart } from "./themeBundleCore";

const MS_PER_MINUTE = 60_000;
/** Owner ruling rc-eb8b78674007: a dark↔light move switches colours within
 * about 3 s at the middle of the transition, so text never sits on a
 * background of nearly its own luminance for minutes. */
export const SCHEME_SWAP_MS = 3_000;

const HHMM_RE = /^([01]\d|2[0-3]):([0-5]\d)$/;

export function parseDaypartStart(start: string): { hour: number; minute: number } | null {
  const m = HHMM_RE.exec(start);
  return m ? { hour: Number(m[1]), minute: Number(m[2]) } : null;
}

/** That wall-clock time on the local calendar day of `day` (a DST gap moves it forward). */
function startOn(day: Date, start: string, dayOffset: number): Date {
  const hm = parseDaypartStart(start)!;
  return new Date(day.getFullYear(), day.getMonth(), day.getDate() + dayOffset, hm.hour, hm.minute);
}

export interface DaypartLook {
  colors: Record<string, string>;
  backgrounds: ThemeBundle["backgrounds"];
  backgroundModes: ThemeBundle["backgroundModes"];
}

/** The part layered over the bundle's top-level look. */
export function daypartLook(bundle: ThemeBundle, part: ThemeDaypart): DaypartLook {
  const backgrounds = { ...bundle.backgrounds, ...part.backgrounds };
  const backgroundModes = { ...bundle.backgroundModes, ...part.backgroundModes };
  return {
    colors: { ...bundle.colors, ...part.colors },
    backgrounds: Object.keys(backgrounds).length ? backgrounds : undefined,
    backgroundModes: Object.keys(backgroundModes).length ? backgroundModes : undefined,
  };
}

export interface DaypartPosition {
  /** Index of the part being moved away from (equal to `to` once settled). */
  from: number;
  /** Index of the part currently starting or shown. */
  to: number;
  /** When the move into `to` began. */
  startedAt: Date;
  /** Milliseconds since `startedAt`. */
  elapsedMs: number;
  transitionMs: number;
  /** When the next part starts. */
  nextStart: Date;
}

/** Where `now` sits among the bundle's parts, by the viewer's local clock. */
export function daypartPositionAt(bundle: ThemeBundle, now: Date): DaypartPosition | null {
  const dp = bundle.dayparts;
  if (!dp || dp.parts.length < 2) return null;
  const t = now.getTime();
  const n = dp.parts.length;
  let latest: { index: number; at: Date } | null = null;
  let next: Date | null = null;
  for (const dayOffset of [-1, 0, 1]) {
    dp.parts.forEach((p, index) => {
      const at = startOn(now, p.start, dayOffset);
      if (at.getTime() <= t) {
        if (!latest || at.getTime() > latest.at.getTime()) latest = { index, at };
      } else if (!next || at.getTime() < next.getTime()) {
        next = at;
      }
    });
  }
  const cur = latest as { index: number; at: Date } | null;
  if (!cur || !next) return null;
  return {
    from: (cur.index - 1 + n) % n,
    to: cur.index,
    startedAt: cur.at,
    elapsedMs: t - cur.at.getTime(),
    transitionMs: dp.transitionMinutes * MS_PER_MINUTE,
    nextStart: next,
  };
}

export function inTransition(pos: DaypartPosition): boolean {
  return pos.elapsedMs < pos.transitionMs;
}

/** Colour progress 0..1: the whole transition for same-scheme parts, the
 * 3-second window at its middle for a dark↔light move. */
export function colorProgress(pos: DaypartPosition, schemeFlips: boolean): number {
  if (!inTransition(pos)) return 1;
  if (!schemeFlips) return pos.elapsedMs / pos.transitionMs;
  const swapStart = pos.transitionMs / 2 - SCHEME_SWAP_MS / 2;
  return Math.min(1, Math.max(0, (pos.elapsedMs - swapStart) / SCHEME_SWAP_MS));
}

function lookIsDark(look: DaypartLook): boolean | null {
  const bg = look.colors["--color-bg"];
  const c = bg ? parseColor(bg) : null;
  return c ? isDarkColor(c) : null;
}

export function mixColors(
  from: Record<string, string>,
  to: Record<string, string>,
  progress: number
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const tok of new Set([...Object.keys(from), ...Object.keys(to)])) {
    const a = from[tok] !== undefined ? parseColor(from[tok]) : null;
    const b = to[tok] !== undefined ? parseColor(to[tok]) : null;
    if (from[tok] !== undefined && from[tok] === to[tok]) out[tok] = from[tok];
    else if (progress <= 0 && from[tok] !== undefined) out[tok] = from[tok];
    else if (progress >= 1 && to[tok] !== undefined) out[tok] = to[tok];
    else if (a && b) out[tok] = formatColor(mixColor(a, b, progress));
    else {
      const v = progress < 0.5 ? from[tok] : to[tok];
      if (v !== undefined) out[tok] = v;
    }
  }
  return out;
}

export interface DaypartFrame {
  /** The look to apply now: interpolated colours, the outgoing part's background. */
  base: ThemeBundle;
  /** A second canvas layer drawn over the base background during a move
   * between two different images; null when settled or the image does not change. */
  overlay: { canvas: string; mode: BackgroundMode | undefined; opacity: number } | null;
  /** When the frame needs recomputing next. */
  nextUpdate: Date;
}

/** Colour refresh cadence during a same-scheme transition. */
export const GRADUAL_COLOR_STEP_MS = 15_000;
/** Colour refresh cadence inside the 3-second dark↔light swap. */
export const SWAP_COLOR_STEP_MS = 100;

const CANVAS: BackgroundKey = "canvas";

/** The bundle with its parts resolved for `now`; the bundle itself when it has no parts. */
export function daypartFrameAt(bundle: ThemeBundle, now: Date): DaypartFrame | null {
  const pos = daypartPositionAt(bundle, now);
  if (!pos) return null;
  const parts = bundle.dayparts!.parts;
  const fromLook = daypartLook(bundle, parts[pos.from]);
  const toLook = daypartLook(bundle, parts[pos.to]);
  const settled = !inTransition(pos);
  const fromDark = lookIsDark(fromLook);
  const toDark = lookIsDark(toLook);
  const flips = fromDark !== null && toDark !== null && fromDark !== toDark;
  const p = colorProgress(pos, flips);
  const { dayparts: _drop, ...rest } = bundle;
  void _drop;
  const fromCanvas = fromLook.backgrounds?.[CANVAS];
  const toCanvas = toLook.backgrounds?.[CANVAS];
  const sameBackground =
    fromCanvas === toCanvas && fromLook.backgroundModes?.[CANVAS] === toLook.backgroundModes?.[CANVAS];
  const fade = pos.elapsedMs / pos.transitionMs;
  // The image being faded in rides the overlay; when the incoming part has no
  // image, the outgoing one rides it instead and fades out.
  const fadeOut = !settled && !sameBackground && !toCanvas;
  const shownBg = settled || fadeOut ? toLook : fromLook;
  const base: ThemeBundle = {
    ...rest,
    colors: mixColors(fromLook.colors, toLook.colors, p),
    backgrounds: shownBg.backgrounds,
    backgroundModes: shownBg.backgroundModes,
  };
  if (!base.backgrounds) delete base.backgrounds;
  if (!base.backgroundModes) delete base.backgroundModes;
  const overlay =
    settled || sameBackground
      ? null
      : fadeOut
        ? { canvas: fromCanvas!, mode: fromLook.backgroundModes?.[CANVAS], opacity: 1 - fade }
        : { canvas: toCanvas!, mode: toLook.backgroundModes?.[CANVAS], opacity: fade };

  let nextUpdate: Date;
  if (settled) {
    nextUpdate = pos.nextStart;
  } else {
    const end = pos.startedAt.getTime() + pos.transitionMs;
    let step = GRADUAL_COLOR_STEP_MS;
    if (flips) {
      const swapStart = pos.startedAt.getTime() + pos.transitionMs / 2 - SCHEME_SWAP_MS / 2;
      const swapEnd = swapStart + SCHEME_SWAP_MS;
      const t = now.getTime();
      step = t < swapStart ? swapStart - t : t < swapEnd ? SWAP_COLOR_STEP_MS : end - t;
    }
    nextUpdate = new Date(Math.min(end, now.getTime() + Math.max(1, step)));
  }
  return { base, overlay, nextUpdate };
}
