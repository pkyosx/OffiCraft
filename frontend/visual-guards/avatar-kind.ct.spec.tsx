// GUARD (T-3738) — the avatar-KIND mapping at each render site.
//
// Bug (owner acceptance of the custom-theme avatars feature): under a theme
// carrying BOTH per-member-type images, an 外包 worker showed the 正職 image in
// the chat header, and the office rail's 外包 row showed the built-in briefcase
// glyph instead of the 外包 image. Root cause was ② render mapping, not data:
// each site handed Avatar the wrong `kind` (or bypassed Avatar entirely).
//
// jsdom cannot see which IMAGE a real browser paints for a resolved theme avatar
// (activeAvatars is context data; the <img src> is a real-DOM fact). These guards
// mount the REAL components under a themed context and assert the ACTUAL src, so
// a wrong-kind mutant paints the wrong image and reddens the matching test.
//
// MUTANTS (each verified red on exactly its own test; see task report):
//   * OutsourcePanel `kind="outsource"` → `kind="member"`   reddens ONLY
//     "rail: outsource row paints the outsource image".
//   * OutsourcePanel `<Avatar…/>` → `<BriefcaseIcon/>` (the original bug)
//     removes the img entirely → same test reddens (no src to match).
//   * WorkerDetailPanel `<Avatar kind="outsource"/>` → `kind="member"` reddens
//     ONLY "worker detail: identity paints the outsource image"; restoring the
//     hard `<BriefcaseIcon/>` removes the img entirely → same test reddens.
//   * ChatArea header `member.id.startsWith("ow-") ? "outsource" : "member"`
//     → hard `"member"` reddens ONLY "chat header: outsource peer …".
//   * The 正職 tests stay green under those mutants and redden only if a
//     member site is flipped to outsource — so no assertion masks another.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  AvatarRailStory,
  WorkerDetailStory,
  ChatHeaderOutsourceStory,
  ChatHeaderMemberStory,
} from "./stories/AvatarKindStory";
import { MEMBER_IMG, OUTSOURCE_IMG } from "./stories/avatarKindImages";

for (const width of [1280, 390]) {
  test(`width ${width} · rail: outsource row paints the outsource image`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<AvatarRailStory />);
    await expect(
      cmp.locator('[data-testid="outsource-detail-ow-1"] .avatar__img'),
    ).toHaveAttribute("src", OUTSOURCE_IMG);
  });

  test(`width ${width} · rail: 正職 member card paints the member image`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<AvatarRailStory />);
    await expect(
      cmp.locator(".member-card__avatar .avatar__img"),
    ).toHaveAttribute("src", MEMBER_IMG);
  });

  test(`width ${width} · worker detail: identity paints the outsource image`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<WorkerDetailStory />);
    await expect(
      cmp.locator(".mp-identity .avatar__img"),
    ).toHaveAttribute("src", OUTSOURCE_IMG);
  });

  test(`width ${width} · chat header: outsource peer paints the outsource image`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatHeaderOutsourceStory />);
    await expect(cmp.locator(".chat__header .avatar__img")).toHaveAttribute(
      "src",
      OUTSOURCE_IMG,
    );
  });

  test(`width ${width} · chat header: 正職 peer paints the member image`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatHeaderMemberStory />);
    await expect(cmp.locator(".chat__header .avatar__img")).toHaveAttribute(
      "src",
      MEMBER_IMG,
    );
  });
}
