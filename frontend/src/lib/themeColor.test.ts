import { describe, expect, it } from "vitest";
import {
  compositeOver,
  contrastRatio,
  formatColor,
  isDarkColor,
  mixColor,
  parseColor,
} from "./themeColor";

describe("parseColor", () => {
  it.each([
    ["#fff", { r: 255, g: 255, b: 255, a: 1 }],
    ["#0f08", { r: 0, g: 255, b: 0, a: 136 / 255 }],
    ["#1A2b3C", { r: 26, g: 43, b: 60, a: 1 }],
    ["#15101fcc", { r: 21, g: 16, b: 31, a: 204 / 255 }],
    ["rgb(10, 20, 30)", { r: 10, g: 20, b: 30, a: 1 }],
    ["rgba(10,20,30,0.5)", { r: 10, g: 20, b: 30, a: 0.5 }],
    ["rgb(10 20 30 / 25%)", { r: 10, g: 20, b: 30, a: 0.25 }],
    ["rgb(100%, 0%, 50%)", { r: 255, g: 0, b: 127.5, a: 1 }],
    ["hsl(0, 100%, 50%)", { r: 255, g: 0, b: 0, a: 1 }],
    ["hsla(120deg 100% 25% / 0.5)", { r: 0, g: 127.5, b: 0, a: 0.5 }],
    ["hsl(0.5turn, 100%, 50%)", { r: 0, g: 255, b: 255, a: 1 }],
    ["transparent", { r: 0, g: 0, b: 0, a: 0 }],
  ])("reads %s as its rgba channels", (input, expected) => {
    const got = parseColor(input)!;
    expect(got.r).toBeCloseTo(expected.r, 5);
    expect(got.g).toBeCloseTo(expected.g, 5);
    expect(got.b).toBeCloseTo(expected.b, 5);
    expect(got.a).toBeCloseTo(expected.a, 5);
  });

  it.each(["#12345", "#1234567", "rgb(1, 2)", "red", "var(--x)", "hsl(1x, 2%, 3%)", ""])(
    "returns null for %s",
    (input) => {
      expect(parseColor(input)).toBeNull();
    }
  );
});

describe("formatColor", () => {
  it("writes rounded rgba() with alpha to three decimals", () => {
    expect(formatColor({ r: 10.4, g: 20.6, b: 300, a: 0.12345 })).toBe("rgba(10, 21, 255, 0.123)");
  });
});

describe("mixColor", () => {
  const black = { r: 0, g: 0, b: 0, a: 1 };
  const white = { r: 255, g: 255, b: 255, a: 0 };

  it("returns the endpoints at 0 and 1 and the midpoint at 0.5", () => {
    expect(mixColor(black, white, 0)).toEqual(black);
    expect(mixColor(black, white, 1)).toEqual(white);
    expect(mixColor(black, white, 0.5)).toEqual({ r: 127.5, g: 127.5, b: 127.5, a: 0.5 });
  });

  it("clamps progress outside 0..1 to the nearer endpoint", () => {
    expect(mixColor(black, white, -1)).toEqual(black);
    expect(mixColor(black, white, 2)).toEqual(white);
  });
});

describe("contrastRatio", () => {
  it("is 21 for black on white and 1 for equal colours", () => {
    const black = { r: 0, g: 0, b: 0, a: 1 };
    const white = { r: 255, g: 255, b: 255, a: 1 };
    expect(contrastRatio(black, white)).toBeCloseTo(21, 5);
    expect(contrastRatio(white, black)).toBeCloseTo(21, 5);
    expect(contrastRatio(white, white)).toBe(1);
  });
});

describe("isDarkColor", () => {
  it("splits at relative luminance 0.2", () => {
    expect(isDarkColor(parseColor("#767676")!)).toBe(true);
    expect(isDarkColor(parseColor("#7c7c7c")!)).toBe(false);
  });
});

describe("compositeOver", () => {
  it("blends a translucent colour over an opaque backdrop", () => {
    expect(compositeOver({ r: 255, g: 0, b: 0, a: 0.5 }, { r: 0, g: 0, b: 255, a: 1 })).toEqual({
      r: 127.5,
      g: 0,
      b: 127.5,
      a: 1,
    });
  });
});
