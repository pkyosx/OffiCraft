import { MOBILE_BREAKPOINT } from "../hooks/useIsMobile";
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

function dataUriBlob(dataUri: string): Blob {
  const mime = /^data:([^;,]*)/.exec(dataUri)?.[1] ?? "";
  const bin = atob(base64Payload(dataUri) ?? "");
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new Blob([bytes], { type: mime });
}

const STILL_TYPE = "image/webp";
const STILL_QUALITY = 0.92;

async function encodeFrame(frame: ImageBitmap): Promise<Blob> {
  if (typeof OffscreenCanvas !== "undefined") {
    const canvas = new OffscreenCanvas(frame.width, frame.height);
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("no 2d context");
    ctx.drawImage(frame, 0, 0);
    return canvas.convertToBlob({ type: STILL_TYPE, quality: STILL_QUALITY });
  }
  const canvas = document.createElement("canvas");
  canvas.width = frame.width;
  canvas.height = frame.height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("no 2d context");
  ctx.drawImage(frame, 0, 0);
  return new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("toBlob"))), STILL_TYPE, STILL_QUALITY)
  );
}

async function convertToStill(dataUri: string): Promise<string> {
  const frame = await createImageBitmap(dataUriBlob(dataUri));
  try {
    return URL.createObjectURL(await encodeFrame(frame));
  } finally {
    frame.close();
  }
}

const stills = new Map<string, Promise<string | null>>();

/** A blob: URL of the image's first frame, converted once per page per image;
 * null when the browser cannot decode or re-encode it. Never rejects. */
export function stillFrameUrl(dataUri: string): Promise<string | null> {
  let still = stills.get(dataUri);
  if (!still) {
    still = convertToStill(dataUri).catch(() => null);
    stills.set(dataUri, still);
  }
  return still;
}

/** While the viewport is phone-width, write the first frame of an animated
 * canvas into CANVAS_STILL_TOKEN (same layering as --canvas-bg-image) and
 * report the token through `onApplied`. A wider viewport converts lazily when
 * it first narrows. Returns the cancel function; nothing is written after it. */
export function watchMobileStillCanvas(
  root: HTMLElement,
  bundle: ThemeBundle,
  onApplied: (token: string) => void
): () => void {
  const canvas = bundle.backgrounds?.canvas;
  if (!canvas || typeof window.matchMedia !== "function" || !isAnimatedImage(canvas)) {
    return () => {};
  }
  const mode = bundle.backgroundModes?.canvas ?? DEFAULT_BACKGROUND_MODE;
  const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`);
  let live = true;
  const convert = () => {
    mql.removeEventListener("change", onChange);
    void stillFrameUrl(canvas).then((still) => {
      if (!live || !still) return;
      root.style.setProperty(CANVAS_STILL_TOKEN, canvasLayout(still, mode).image);
      onApplied(CANVAS_STILL_TOKEN);
    });
  };
  const onChange = (e: MediaQueryListEvent) => {
    if (e.matches) convert();
  };
  if (mql.matches) convert();
  else mql.addEventListener("change", onChange);
  return () => {
    live = false;
    mql.removeEventListener("change", onChange);
  };
}
