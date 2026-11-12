import { DEFAULT_BACKGROUND_MODE, type ThemeBundle } from "./themeBundleCore";
import { canvasLayout } from "./themePaint";

export const CANVAS_STILL_TOKEN = "--canvas-bg-image-still";

const DATA_URI = /^data:[^;,]*;base64,/;

function base64Payload(dataUri: string): string | null {
  const m = DATA_URI.exec(dataUri);
  return m ? dataUri.slice(m[0].length) : null;
}

function decodeBytes(b64: string, start: number, length: number): Uint8Array | null {
  const first = Math.floor(start / 3) * 4;
  const last = Math.ceil((start + length) / 3) * 4;
  if (last > b64.length) return null;
  let bin: string;
  try {
    bin = atob(b64.slice(first, last));
  } catch {
    return null;
  }
  const skip = start - (first / 4) * 3;
  if (bin.length < skip + length) return null;
  const out = new Uint8Array(length);
  for (let i = 0; i < length; i++) out[i] = bin.charCodeAt(skip + i);
  return out;
}

function ascii(bytes: Uint8Array, from: number, to: number): string {
  return String.fromCharCode(...bytes.subarray(from, to));
}

const PNG_SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];

function isAnimatedPng(b64: string): boolean {
  let at = PNG_SIGNATURE.length;
  for (;;) {
    const head = decodeBytes(b64, at, 8);
    if (!head) return false;
    const type = ascii(head, 4, 8);
    if (type === "acTL") return true;
    if (type === "IDAT" || type === "IEND") return false;
    const len = ((head[0] << 24) | (head[1] << 16) | (head[2] << 8) | head[3]) >>> 0;
    at += 12 + len;
  }
}

/** True for an animated WebP (VP8X with the animation flag) or an APNG
 * (acTL before the first IDAT); false for everything else, including input
 * that is not a base64 data URI. */
export function isAnimatedImage(dataUri: string): boolean {
  const b64 = base64Payload(dataUri);
  if (!b64) return false;
  const head = decodeBytes(b64, 0, 21);
  if (!head) return false;
  if (ascii(head, 0, 4) === "RIFF" && ascii(head, 8, 12) === "WEBP") {
    return ascii(head, 12, 16) === "VP8X" && (head[20] & 0x02) !== 0;
  }
  if (PNG_SIGNATURE.every((b, i) => head[i] === b)) return isAnimatedPng(b64);
  return false;
}

const STILL_TYPE = "image/webp";
const STILL_QUALITY = 0.92;
const PROBE_SIZE = 16;

type Ctx2D = Pick<CanvasRenderingContext2D, "drawImage" | "getImageData">;

function canvas2d(width: number, height: number): { ctx: Ctx2D; encode: () => Promise<Blob> } {
  if (typeof OffscreenCanvas !== "undefined") {
    const canvas = new OffscreenCanvas(width, height);
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("no 2d context");
    return { ctx, encode: () => canvas.convertToBlob({ type: STILL_TYPE, quality: STILL_QUALITY }) };
  }
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("no 2d context");
  return {
    ctx,
    encode: () =>
      new Promise((resolve, reject) =>
        canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("toBlob"))), STILL_TYPE, STILL_QUALITY)
      ),
  };
}

// A decoder may hand back a fully transparent frame without an error (a Safari
// risk); showing it would blank the canvas instead of leaving the animation.
function isBlank(frame: ImageBitmap): boolean {
  const { ctx } = canvas2d(PROBE_SIZE, PROBE_SIZE);
  ctx.drawImage(frame, 0, 0, PROBE_SIZE, PROBE_SIZE);
  const { data } = ctx.getImageData(0, 0, PROBE_SIZE, PROBE_SIZE);
  for (let i = 3; i < data.length; i += 4) if (data[i] > 0) return false;
  return true;
}

async function convertToStill(dataUri: string): Promise<string | null> {
  const frame = await createImageBitmap(await (await fetch(dataUri)).blob());
  try {
    if (isBlank(frame)) return null;
    const { ctx, encode } = canvas2d(frame.width, frame.height);
    ctx.drawImage(frame, 0, 0);
    return URL.createObjectURL(await encode());
  } finally {
    frame.close();
  }
}

const stills = new Map<string, Promise<string | null>>();

/** A blob: URL of the image's first frame, converted once per page per image;
 * null when the browser cannot decode or re-encode it, or the frame is fully
 * transparent. Never rejects. */
export function stillFrameUrl(dataUri: string): Promise<string | null> {
  let still = stills.get(dataUri);
  if (!still) {
    still = convertToStill(dataUri).catch(() => null);
    stills.set(dataUri, still);
  }
  return still;
}

export const BG_MOTIONS = ["auto", "play", "still"] as const;
export type BgMotion = (typeof BG_MOTIONS)[number];

/** When "auto" shows the still frame: a touch device (primary input cannot
 * hover), whatever its width or orientation. global.css's still rule repeats
 * it as a media query; change both together. */
export const AUTO_STILL_QUERY = "(hover: none)";

/** Write the first frame of an animated canvas into CANVAS_STILL_TOKEN (same
 * layering as --canvas-bg-image) and report the token through `onApplied`:
 * at once for "still", never for "play", and for "auto" while AUTO_STILL_QUERY
 * matches, converting lazily when it first does. Returns the cancel function;
 * nothing is written after it. */
export function watchStillCanvas(
  root: HTMLElement,
  bundle: ThemeBundle,
  motion: BgMotion,
  onApplied: (token: string) => void
): () => void {
  const canvas = bundle.backgrounds?.canvas;
  if (motion === "play" || !canvas || !isAnimatedImage(canvas)) return () => {};
  if (motion === "auto" && typeof window.matchMedia !== "function") return () => {};
  const mode = bundle.backgroundModes?.canvas ?? DEFAULT_BACKGROUND_MODE;
  let live = true;
  const convert = () => {
    void stillFrameUrl(canvas).then((still) => {
      if (!live || !still) return;
      root.style.setProperty(CANVAS_STILL_TOKEN, canvasLayout(still, mode).image);
      onApplied(CANVAS_STILL_TOKEN);
    });
  };
  if (motion === "still") {
    convert();
    return () => {
      live = false;
    };
  }
  const mql = window.matchMedia(AUTO_STILL_QUERY);
  const onChange = (e: MediaQueryListEvent) => {
    if (!e.matches) return;
    mql.removeEventListener("change", onChange);
    convert();
  };
  if (mql.matches) convert();
  else mql.addEventListener("change", onChange);
  return () => {
    live = false;
    mql.removeEventListener("change", onChange);
  };
}
