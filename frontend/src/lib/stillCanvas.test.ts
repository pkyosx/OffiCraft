import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CANVAS_STILL_TOKEN, isAnimatedImage, stillFrameUrl, watchStillCanvas } from "./stillCanvas";
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

type StubFrame = { width: number; height: number; alpha: number; close(): void };

function stubFrameEncoder(): { convertedFrames: () => number } {
  let converted = 0;
  vi.stubGlobal("createImageBitmap", async (blob: Blob) => ({
    width: 4,
    height: 3,
    alpha: (await blob.text()).includes("TRANSPARENT") ? 0 : 255,
    close() {},
  }));
  vi.stubGlobal(
    "OffscreenCanvas",
    class {
      constructor(
        readonly width: number,
        readonly height: number
      ) {}
      getContext() {
        let drawn: StubFrame | undefined;
        const { width, height } = this;
        return {
          drawImage(frame: StubFrame) {
            drawn = frame;
          },
          getImageData() {
            const data = new Uint8ClampedArray(width * height * 4);
            for (let i = 3; i < data.length; i += 4) data[i] = drawn?.alpha ?? 0;
            return { data };
          },
        };
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

  it("resolves null when the browser cannot decode the image, and a blob URL once it can", async () => {
    vi.stubGlobal("createImageBitmap", async () => {
      throw new DOMException("bad image", "InvalidStateError");
    });
    expect(await stillFrameUrl(dataUri("image/webp", riff("VP8X", [0x02, 1, 0, 0, 0, 0, 0, 0, 0, 0])))).toBeNull();
    stubFrameEncoder();
    expect(await stillFrameUrl(dataUri("image/webp", riff("VP8X", [0x02, 2, 0, 0, 0, 0, 0, 0, 0, 0])))).toBe("blob:still-1");
  });

  it("resolves null for a fully transparent first frame without encoding it, and a blob URL for a visible one", async () => {
    const enc = stubFrameEncoder();
    const transparent = riff("VP8X", [0x02, 3, 0, 0, 0, 0, 0, 0, 0, 0]).concat([..."TRANSPARENT"].map((c) => c.charCodeAt(0)));
    expect(await stillFrameUrl(dataUri("image/webp", transparent))).toBeNull();
    expect(enc.convertedFrames()).toBe(0);
    expect(await stillFrameUrl(dataUri("image/webp", riff("VP8X", [0x02, 4, 0, 0, 0, 0, 0, 0, 0, 0])))).toBe("blob:still-1");
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

describe("watchStillCanvas", () => {
  it("auto at phone width writes the still with the theme's layering and reports the token", async () => {
    stubFrameEncoder();
    stubViewport(true);
    const root = document.createElement("div");
    const applied: string[] = [];
    watchStillCanvas(root, animatedBundle(10, "sides"), "auto", (t) => applied.push(t));
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1"), url("blob:still-1")');
    expect(applied).toEqual([CANVAS_STILL_TOKEN]);
  });

  it("auto on a desktop viewport converts only once the viewport narrows to phone width", async () => {
    const enc = stubFrameEncoder();
    const viewport = stubViewport(false);
    const root = document.createElement("div");
    watchStillCanvas(root, animatedBundle(11), "auto", () => {});
    await flush();
    expect(enc.convertedFrames()).toBe(0);
    viewport.narrow();
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1")');
  });

  it("still on a desktop viewport writes the still at once and keeps it after the viewport narrows", async () => {
    const enc = stubFrameEncoder();
    const viewport = stubViewport(false);
    const root = document.createElement("div");
    const applied: string[] = [];
    watchStillCanvas(root, animatedBundle(14), "still", (t) => applied.push(t));
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1")');
    expect(applied).toEqual([CANVAS_STILL_TOKEN]);
    viewport.narrow();
    await flush();
    expect(enc.convertedFrames()).toBe(1);
    expect(applied).toEqual([CANVAS_STILL_TOKEN]);
  });

  it("play writes nothing at phone width, nor after the viewport narrows", async () => {
    const enc = stubFrameEncoder();
    const viewport = stubViewport(false);
    const phone = document.createElement("div");
    const narrowed = document.createElement("div");
    watchStillCanvas(narrowed, animatedBundle(15), "play", () => {});
    viewport.narrow();
    watchStillCanvas(phone, animatedBundle(15), "play", () => {});
    await flush();
    expect(phone.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(narrowed.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(enc.convertedFrames()).toBe(0);
  });

  it("after cancel writes nothing in auto or still, while an uncancelled watch on the same image does", async () => {
    stubFrameEncoder();
    stubViewport(true);
    const cancelled = document.createElement("div");
    const cancelledStill = document.createElement("div");
    const kept = document.createElement("div");
    const applied: string[] = [];
    watchStillCanvas(cancelled, animatedBundle(12), "auto", (t) => applied.push(t))();
    watchStillCanvas(cancelledStill, animatedBundle(12), "still", (t) => applied.push(t))();
    watchStillCanvas(kept, animatedBundle(12), "auto", () => {});
    await flush();
    expect(cancelled.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(cancelledStill.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(applied).toEqual([]);
    expect(kept.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe('url("blob:still-1")');
  });

  it("leaves a still-image canvas alone at phone width", async () => {
    const enc = stubFrameEncoder();
    stubViewport(true);
    const root = document.createElement("div");
    const still = { ...animatedBundle(13), backgrounds: { canvas: dataUri("image/webp", riff("VP8 ", Array(10).fill(13))) } };
    watchStillCanvas(root, still, "auto", () => {});
    watchStillCanvas(document.createElement("div"), animatedBundle(13), "auto", () => {});
    await flush();
    expect(root.style.getPropertyValue(CANVAS_STILL_TOKEN)).toBe("");
    expect(enc.convertedFrames()).toBe(1);
  });
});
