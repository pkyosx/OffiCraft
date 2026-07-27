// T-c826 real-browser guard for personal member avatars. It verifies the same
// state matrix at the requested mobile, tablet, and desktop widths and writes
// owner-facing evidence when AVATAR_EDITOR_SHOT_DIR is provided.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { AvatarEditorStory } from "./stories/AvatarEditorStory";
import { MEMBER_IMG } from "./stories/avatarKindImages";
import { WorkerDetailPanelTaskOrderStory } from "./stories/WorkerDetailPanelTaskOrderStory";

const SHOT_DIR =
  process.env.AVATAR_EDITOR_SHOT_DIR ?? "test-results/avatar-editor";
const PNG_FILE = {
  name: "member-avatar.png",
  mimeType: "image/png",
  buffer: Buffer.from([
    0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
  ]),
};

async function upload(editor: Locator) {
  await editor.locator('input[type="file"]').setInputFiles(PNG_FILE);
}

async function expectNoHorizontalOverflow(page: Page, width: number) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);

  const matrix = page.getByTestId("avatar-editor-matrix");
  const box = await matrix.boundingBox();
  expect(box, "avatar editor matrix box").not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
}

for (const width of [390, 768, 1280]) {
  test(`width ${width}: avatar editor covers success, loading, error, and keyboard focus`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<AvatarEditorStory />);

    const success = cmp.getByTestId("success-editor");
    await expect(success.locator(".avatar__img")).toHaveCount(0);

    // The visually hidden native input must not create an invisible keyboard
    // stop. Tab lands on the visible action and its production focus ring paints.
    await page.keyboard.press("Tab");
    const visibleUpload = success.locator("button.avatar-editor__button");
    await expect(visibleUpload).toBeFocused();
    await expect(visibleUpload).toHaveCSS("outline-style", "solid");

    await upload(success);
    await expect(success.locator(".avatar__img")).toHaveAttribute(
      "src",
      MEMBER_IMG,
    );
    await expect(
      success.locator("button.avatar-editor__button--remove"),
    ).toBeVisible();

    const loading = cmp.getByTestId("loading-editor");
    await upload(loading);
    const loadingButton = loading.locator("button.avatar-editor__button");
    await expect(loadingButton).toHaveText("處理中…");
    await expect(loadingButton).toBeDisabled();

    const error = cmp.getByTestId("error-editor");
    await upload(error);
    await expect(error.getByRole("alert")).toHaveText(
      "頭像儲存失敗，請稍後重試",
    );
    await expect(
      error.locator("button.avatar-editor__button"),
    ).toBeEnabled();

    await expectNoHorizontalOverflow(page, width);
    await page.screenshot({
      path: `${SHOT_DIR}/avatar-editor-${width}.png`,
      fullPage: true,
    });
  });
}

test("390px: real worker detail identity holds the avatar editor without overflow", async ({
  mount,
  page,
}) => {
  const width = 390;
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<WorkerDetailPanelTaskOrderStory />);
  const identity = cmp.locator(".mp-identity");
  await expect(identity).toBeVisible();
  await expect(
    identity.locator("button.avatar-editor__button--remove"),
  ).toBeVisible();
  const box = await identity.boundingBox();
  expect(box, "real worker identity box").not.toBeNull();
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
});
