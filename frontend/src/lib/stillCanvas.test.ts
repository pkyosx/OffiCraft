import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CANVAS_STILL_TOKEN, isAnimatedImage, stillFrameUrl, watchMobileStillCanvas } from "./stillCanvas";
import { TINY_PNG } from "./paintFixtures";
import type { ThemeBundle } from "./themeBundleCore";

function realCanvas(file: string): string {
  const b = JSON.parse(readFileSync(resolve(__dirname, "../../../themes", file), "utf8")) as ThemeBundle;
  return b.backgrounds!.canvas!;
}

function chunk(type: string, data: number[] = []): number[] {
  const n = data.length;
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255, ...[...type].map((c) => c.charCodeAt(0)), ...data, 0, 0, 0, 0];
}

function dataUri(mime: string, bytes: number[]): string {
  return `data:${mime};base64,${btoa(String.fromCharCode(...bytes))}`;
}

const PNG_SIG = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
const IHDR = chunk("IHDR", [0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0]);
const ACTL = chunk("acTL", [0, 0, 0, 2, 0, 0, 0, 0]);
const IDAT = chunk("IDAT", [0x78, 0x9c, 0x63, 0, 0, 0, 2, 0, 1]);
const IEND = chunk("IEND");
const TEXT = chunk("tEXt", Array.from({ length: 300 }, () => 0x41));

function riff(fourcc: string, payload: number[]): number[] {
  const p = payload.length;
  const body = [..."WEBP", ...fourcc].map((c) => c.charCodeAt(0)).concat([p & 255, (p >>> 8) & 255, 0, 0], payload);
  const n = body.length;
  return [..."RIFF"].map((c) => c.charCodeAt(0)).concat([n & 255, (n >>> 8) & 255, (n >>> 16) & 255, n >>> 24], body);
}

describe("isAnimatedImage", () => {
  it("is true for an animated WebP, including the shipped desk and seasonal canvases", () => {
    expect(isAnimatedImage(realCanvas("modern-office-desk-day.theme.json"))).toBe(true);
    expect(isAnimatedImage(realCanvas("thanksgiving-2026-night.theme.json"))).toBe(true);
    expect(isAnimatedImage(dataUri("image/webp", riff("VP8X", [0x12, 0, 0, 0, 0, 0, 0, 0, 0, 0])))).toBe(true);
  });

  it("is true for an APNG, also when another chunk sits between IHDR and acTL", () => {
    expect(isAnimatedImage(dataUri("image/png", [...PNG_SIG, ...IHDR, ...ACTL, ...IDAT, ...IEND]))).toBe(true);
    expect(isAnimatedImage(dataUri("image/png", [...PNG_SIG, ...IHDR, ...TEXT, ...ACTL, ...IDAT, ...IEND]))).toBe(true);
  });

  it("is false for still PNG, JPEG and WebP images", () => {
    expect(isAnimatedImage(dataUri("image/png", [...PNG_SIG, ...IHDR, ...IDAT, ...IEND]))).toBe(false);
    expect(isAnimatedImage(dataUri("image/png", [...PNG_SIG, ...IHDR, ...IDAT, ...ACTL, ...IEND]))).toBe(false);
    expect(isAnimatedImage(TINY_PNG)).toBe(false);
    expect(isAnimatedImage("data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcG")).toBe(false);
    expect(isAnimatedImage(dataUri("image/webp", riff("VP8 ", Array(10).fill(0))))).toBe(false);
    expect(isAnimatedImage(dataUri("image/webp", riff("VP8X", [0x10, 0, 0, 0, 0, 0, 0, 0, 0, 0])))).toBe(false);
  });
});

function stubFrameEncoder(): { convertedFrames: () => number } {
  let converted = 0;
  vi.stubGlobal("createImageBitmap", async () => ({ width: 4, height: 3, close() {} }));
  vi.stubGlobal(
    "OffscreenCanvas",
    class {
      getContext() {
        return { drawImage() {} };
      }
      async convertToBlob() {
        converted++;
        return new Blob(["still"], { type: "image/webp" });
      }
    }
  );
  vi.spyOn(URL, "createObjectURL").mockImplementation(() => `blob:still-${converted}`);
  return { convertedFrames: () => converted };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("stillFrameUrl", () => {
  it("converts an image once per page and hands every caller the same blob URL", async () => {
    const enc = stubFrameEncoder();
    const uri = dataUri("image/webp", riff("VP8X", [0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0]));
    const [a, b] = await Promise.all([stillFrameUrl(uri), stillFrameUrl(uri)]);
    expect(a).toBe("blob:still-1");
    expect(b).toBe("blob:still-1");
    expect(await stillFrameUrl(uri)).toBe("blob:still-1");
    expect(enc.convertedFrames()).toBe(1);
  });

  it("resolves null when the browser cannot decode the image", async () => {
    vi.stubGlobal("createImageBitmap", async () => {
      throw new DOMException("bad image", "InvalidStateError");
    });
    expect(await stillFrameUrl(dataUri("image/webp", riff("VP8X", [0x02, 1, 0, 0, 0, 0, 0, 0, 0, 0])))).toBeNull();
  });
});

function stubViewport(mobile: boolean) {
  const listeners = new Set<(e: MediaQueryListEvent) => void>();
  const mql = {
    matches: mobile,
    addEventListener: (_: string, fn: (e: MediaQueryListEvent) => void) => listeners.add(fn),
    removeEventListener: (_: string, fn: (e: MediaQueryListEvent) => void) => listeners.delete(fn),
  };
  vi.stubGlobal("matchMedia", (q: string) => (q === "(max-width: 720px)" ? mql : { ...mql, matches: false }));
  return {
    narrow() {
      mql.matches = true;
      for (const fn of [...listeners]) fn({ matches: true } as MediaQueryListEvent);
    },
  };
}

function animatedBundle(seed: number, mode?: "sides"): ThemeBundle {
  return {
    id: "anim",
    name: "Anim",
    colors: {},
    backgrounds: { canvas: dataUri("image/webp", riff("VP8X", [0x02, seed, 0, 0, 0, 0, 0, 0, 0, 0])) },
    ...(mode ? { backgroundModes: { canvas: mode } } : {}),
  };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("watchMobileStillCanvas", () => {
  it("at phone width writes the still with the theme's layering and reports the token", async () => {
    stubFrameEncoder();
    stubViewport(true);
    const root = document.createElement("div");
    const applied: string[] = [];
    watchMobileStillCanvas(root, animatedBundle(10, "sides"), (t) => applied.push(t));
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1"), url("blob:still-1")');
    expect(applied).toEqual([CANVAS_STILL_TOKEN]);
  });

  it("on a desktop viewport converts only once the viewport narrows to phone width", async () => {
    const enc = stubFrameEncoder();
    const viewport = stubViewport(false);
    const root = document.createElement("div");
    watchMobileStillCanvas(root, animatedBundle(11), () => {});
    await flush();
    expect(enc.convertedFrames()).toBe(0);
    viewport.narrow();
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1")');
  });

  it("after cancel writes nothing, while an uncancelled watch on the same image does", async () => {
    stubFrameEncoder();
    stubViewport(true);
    const cancelled = document.createElement("div");
    const kept = document.createElement("div");
    const applied: string[] = [];
    watchMobileStillCanvas(cancelled, animatedBundle(12), (t) => applied.push(t))();
    watchMobileStillCanvas(kept, animatedBundle(12), () => {});
    await flush();
    expect(cancelled.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(applied).toEqual([]);
    expect(kept.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1")');
  });

  it("leaves a still-image canvas alone at phone width", async () => {
    const enc = stubFrameEncoder();
    stubViewport(true);
    const root = document.createElement("div");
    const still = { ...animatedBundle(13), backgrounds: { canvas: dataUri("image/webp", riff("VP8 ", Array(10).fill(13))) } };
    watchMobileStillCanvas(root, still, () => {});
    watchMobileStillCanvas(document.createElement("div"), animatedBundle(13), () => {});
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(enc.convertedFrames()).toBe(1);
  });
});
