// T-3 — the shared ID field's keyboard ring must survive the list scrollport.
//
// This mounts the complete App shell (topbar, nav and the selected list page),
// rather than a field-only story. The first field is flush with `.tasks` /
// `.replies`; an outward outline is clipped at that scrollport's top/left edge.
// The screenshot diff below is the pixel-level witness: focused pixels must
// exist on both the upper and left field edges, while no focus pixels may be
// outside the field/scrollport bounds.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { AppPageStory } from "./stories/RepliesFilterPanelStory";

type Box = { x: number; y: number; width: number; height: number };
type PixelShot = {
  clip: { x: number; y: number };
  width: number;
  height: number;
  pixels: number[];
};

async function capturePixels(page: Page, field: Locator): Promise<PixelShot> {
  const box = (await field.boundingBox())!;
  const clip = {
    x: Math.max(0, box.x - 4),
    y: Math.max(0, box.y - 4),
    width: box.width + 8,
    height: box.height + 8,
  };
  const png = await page.screenshot({ clip });
  return await page.evaluate(
    async ({ data, clip }) => {
      const image = new Image();
      image.src = `data:image/png;base64,${data}`;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const context = canvas.getContext("2d")!;
      context.drawImage(image, 0, 0);
      return {
        clip,
        width: canvas.width,
        height: canvas.height,
        pixels: Array.from(
          context.getImageData(0, 0, canvas.width, canvas.height).data
        ),
      };
    },
    { data: png.toString("base64"), clip }
  );
}

function pixelEvidence(
  before: PixelShot,
  after: PixelShot,
  field: Box,
  scrollport: Box
) {
  const fieldLeft = Math.round(field.x - before.clip.x);
  const fieldTop = Math.round(field.y - before.clip.y);
  const fieldRight = Math.round(field.x + field.width - before.clip.x);
  const fieldBottom = Math.round(field.y + field.height - before.clip.y);
  const portLeft = Math.round(scrollport.x - before.clip.x);
  const portTop = Math.round(scrollport.y - before.clip.y);
  const portRight = Math.round(scrollport.x + scrollport.width - before.clip.x);
  const portBottom = Math.round(scrollport.y + scrollport.height - before.clip.y);
  const changed = (x: number, y: number) => {
    const i = (y * before.width + x) * 4;
    return Math.max(
      Math.abs(before.pixels[i] - after.pixels[i]),
      Math.abs(before.pixels[i + 1] - after.pixels[i + 1]),
      Math.abs(before.pixels[i + 2] - after.pixels[i + 2])
    ) > 6;
  };

  let changedPixels = 0;
  let outsideScrollport = 0;
  let upperEdgePixels = 0;
  let leftEdgePixels = 0;
  for (let y = 0; y < before.height; y += 1) {
    for (let x = 0; x < before.width; x += 1) {
      if (!changed(x, y)) continue;
      changedPixels += 1;
      if (
        x < portLeft ||
        x >= portRight ||
        y < portTop ||
        y >= portBottom
      ) {
        outsideScrollport += 1;
      }
      if (
        y >= fieldTop &&
        y < fieldTop + 3 &&
        x >= fieldLeft + 8 &&
        x < fieldRight - 8
      ) {
        upperEdgePixels += 1;
      }
      if (
        x >= fieldLeft &&
        x < fieldLeft + 3 &&
        y >= fieldTop + 8 &&
        y < fieldBottom - 8
      ) {
        leftEdgePixels += 1;
      }
    }
  }
  return {
    changedPixels,
    outsideScrollport,
    upperEdgePixels,
    leftEdgePixels,
  };
}

async function fieldLayout(field: Locator) {
  return await field.evaluate((node) => {
    const input = node as HTMLInputElement;
    const fieldBox = input.getBoundingClientRect();
    const scrollport = input.closest(".tasks, .replies") as HTMLElement | null;
    if (!scrollport) throw new Error("ID field has no list scrollport ancestor");
    const portBox = scrollport.getBoundingClientRect();
    const style = getComputedStyle(input);
    return {
      field: {
        x: fieldBox.x,
        y: fieldBox.y,
        width: fieldBox.width,
        height: fieldBox.height,
      },
      scrollport: {
        x: portBox.x,
        y: portBox.y,
        width: portBox.width,
        height: portBox.height,
      },
      outlineStyle: style.outlineStyle,
      outlineWidth: parseFloat(style.outlineWidth),
      outlineOffset: parseFloat(style.outlineOffset),
      overflowX: getComputedStyle(scrollport).overflowX,
      overflowY: getComputedStyle(scrollport).overflowY,
      inputScrollWidth: input.scrollWidth,
      inputClientWidth: input.clientWidth,
    };
  });
}

async function pageOverflow(page: Page) {
  return await page.evaluate(() => {
    const pageWidth = document.documentElement.scrollWidth - window.innerWidth;
    const listOverflow = Array.from(
      document.querySelectorAll<HTMLElement>(".tasks, .replies")
    ).map((list) => list.scrollWidth - list.clientWidth);
    return { pageWidth, listOverflow };
  });
}

for (const pageName of ["tasks", "replies"] as const) {
  for (const width of [1040, 320]) {
    test(`T-3 ${pageName} width ${width}: focused ring stays inside scrollport`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(
        <AppPageStory
          page={pageName}
          theme="dark"
          wide={width >= 1040}
        />
      );
      const field = cmp.getByTestId(
        pageName === "tasks" ? "filter-task-id" : "filter-reply-card-id"
      );
      await expect(field).toBeVisible();
      await expect(field).toHaveValue("");

      const unfocused = await fieldLayout(field);
      const before = await capturePixels(page, field);

      await field.focus();
      await expect(field).toBeFocused();
      const focused = await fieldLayout(field);
      const after = await capturePixels(page, field);
      const evidence = pixelEvidence(
        before,
        after,
        focused.field,
        focused.scrollport
      );

      // The shared CSS must preserve the input's box and the list's scrolling
      // contract; only the focus paint changes.
      expect(focused.field.x).toBeCloseTo(unfocused.field.x, 4);
      expect(focused.field.y).toBeCloseTo(unfocused.field.y, 4);
      expect(focused.field.width).toBeCloseTo(unfocused.field.width, 4);
      expect(focused.field.height).toBeCloseTo(unfocused.field.height, 4);
      expect(focused.inputScrollWidth - focused.inputClientWidth).toBeLessThanOrEqual(1);
      expect(focused.overflowX).toBe("auto");
      expect(focused.overflowY).toBe("auto");

      // Direct pixel evidence: the upper/left ring is present within the
      // field, and no focus pixels escape the field or its scrollport.
      expect(focused.outlineStyle).toBe("solid");
      expect(focused.outlineWidth).toBeGreaterThan(0);
      expect(focused.outlineOffset).toBe(-1);
      expect(evidence.changedPixels).toBeGreaterThan(0);
      expect(evidence.upperEdgePixels, "focused ring upper-edge pixels").toBeGreaterThan(8);
      expect(evidence.leftEdgePixels, "focused ring left-edge pixels").toBeGreaterThan(8);
      expect(evidence.outsideScrollport, "focus pixels outside list scrollport").toBeLessThanOrEqual(1);

      const overflow = await pageOverflow(page);
      expect(overflow.pageWidth, "page horizontal overflow").toBeLessThanOrEqual(1);
      for (const value of overflow.listOverflow) {
        expect(value, "list horizontal overflow").toBeLessThanOrEqual(1);
      }

      // Keep reproducible before/after evidence available for the task handoff.
      await field.blur();
      await page.screenshot({
        path: `/tmp/t3-id-filter-${pageName}-${width}-unfocused.png`,
        fullPage: true,
      });
      await field.focus();
      await page.screenshot({
        path: `/tmp/t3-id-filter-${pageName}-${width}-focused.png`,
        fullPage: true,
      });
    });
  }
}
