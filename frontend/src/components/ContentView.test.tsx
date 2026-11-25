import { describe, it, expect } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ContentView, type ContentViewProps } from "./ContentView";

const p = zh.chat.mdPreview;

function mount(props: ContentViewProps) {
  return render(
    <I18nProvider>
      <div data-testid="host">
        <ContentView {...props} />
      </div>
    </I18nProvider>,
  );
}

const statusText = (c: HTMLElement) =>
  Array.from(c.querySelectorAll(".md-preview__status")).map((el) => el.textContent);
const malformedText = (c: HTMLElement) =>
  Array.from(c.querySelectorAll(".md-preview__malformed")).map((el) => el.textContent);

describe("ContentView", () => {
  it("renders markdown through the shared document skin, with block images when the host resolves them", () => {
    const { container } = mount({
      kind: "markdown",
      text: "# Title\n\n![shot](/api/chat/attachment/att-1)\n",
      resolveImageSrc: (src) => `${src}?token=t`,
      testId: "md",
    });
    const md = container.querySelector("[data-testid=md]")!;
    expect(md.className).toBe("md-preview__md doc-md");
    expect(md.querySelector("h1")?.textContent).toBe("Title");
    expect(md.querySelector("img")?.getAttribute("src")).toBe("/api/chat/attachment/att-1?token=t");
  });

  it("keeps .doc-md's own page padding only for a host that asks for it", () => {
    const flush = mount({ kind: "markdown", text: "x", testId: "md" });
    expect(flush.getByTestId("md").className).toBe("md-preview__md doc-md");
    flush.unmount();
    const padded = mount({ kind: "markdown", text: "x", padded: true, testId: "md" });
    expect(padded.getByTestId("md").className).toBe("doc-md");
  });

  it("indents JSON, and shows unparseable JSON as stored under one line saying why", () => {
    const ok = mount({ kind: "json", text: '{"a":[1]}' });
    expect(ok.container.querySelector("pre.md-preview__text")?.textContent).toBe('{\n  "a": [\n    1\n  ]\n}');
    expect(malformedText(ok.container)).toEqual([]);
    ok.unmount();
    const bad = mount({ kind: "json", text: "{nope", testId: "body" });
    expect(malformedText(bad.container)).toEqual([p.jsonUnparseable]);
    expect(bad.getByTestId("body").outerHTML).toBe('<pre class="md-preview__text" data-testid="body">{nope</pre>');
  });

  it("indents XML, and shows malformed XML as stored under one line saying why", () => {
    const ok = mount({ kind: "xml", text: "<a><b>1</b></a>" });
    expect(ok.container.querySelector("pre.md-preview__text")?.textContent).toBe("<a>\n  <b>1</b>\n</a>");
    ok.unmount();
    const bad = mount({ kind: "xml", text: "<a><b></a>", testId: "body" });
    expect(malformedText(bad.container)).toEqual([p.xmlUnparseable]);
    expect(bad.getByTestId("body").tagName).toBe("PRE");
    expect(bad.getByTestId("body").textContent).toBe("<a><b></a>");
  });

  it("shows YAML exactly as stored, comments included, with one line saying so when it is not valid YAML", async () => {
    const yaml = "# keep me\nkey:   value\n";
    const good = mount({ kind: "yaml", text: yaml, testId: "body" });
    expect(good.getByTestId("body").textContent).toBe(yaml);
    good.unmount();

    const broken = "a:\n  b: 1\n c: 2\n";
    const bad = mount({ kind: "yaml", text: broken, testId: "body" });
    await waitFor(() => expect(malformedText(bad.container)).toEqual([p.yamlUnparseable]));
    expect(bad.getByTestId("body").textContent).toBe(broken);

    const fixed = "a:\n  b: 1\n  c: 2\n";
    bad.rerender(
      <I18nProvider>
        <div data-testid="host">
          <ContentView kind="yaml" text={fixed} testId="body" />
        </div>
      </I18nProvider>,
    );
    expect(malformedText(bad.container)).toEqual([]);
    expect(bad.getByTestId("body").textContent).toBe(fixed);
  });

  it("lays CSV out as a table with the first row as its header, padding short rows, every cell as text even when it reads as markup", () => {
    const { container } = mount({
      kind: "csv",
      text: 'name,<b>note</b>\nMira,"a, b"\nKyle\n<img src=x onerror=alert(1)>,ok\n',
    });
    const table = container.querySelector(".md-preview__csv.doc-md table")!;
    expect(Array.from(table.querySelectorAll("th")).map((c) => c.textContent)).toEqual(["name", "<b>note</b>"]);
    expect(
      Array.from(table.querySelectorAll("tbody tr")).map((r) =>
        Array.from(r.querySelectorAll("td")).map((c) => c.textContent),
      ),
    ).toEqual([
      ["Mira", "a, b"],
      ["Kyle", ""],
      ["<img src=x onerror=alert(1)>", "ok"],
    ]);
    expect(table.querySelectorAll("img, b").length).toBe(0);
  });

  it("shows a CSV that will not parse as raw text under one line saying why", () => {
    const { container, getByTestId } = mount({ kind: "csv", text: 'a,"b\n1,2', testId: "body" });
    expect(malformedText(container)).toEqual([p.csvUnparseable]);
    expect(container.querySelector("table")).toBeNull();
    expect(getByTestId("body").tagName).toBe("PRE");
    expect(getByTestId("body").textContent).toBe('a,"b\n1,2');
  });

  it("says loading while the text is on its way, and says so when the read failed", () => {
    const loading = mount({ kind: "markdown", text: null });
    expect(statusText(loading.container)).toEqual([p.loading]);
    loading.unmount();
    const failed = mount({ kind: "markdown", text: "x", failed: true });
    expect(statusText(failed.container)).toEqual([p.error]);
  });

  it("frames an HTML page at its share link in the sandbox, never without one", () => {
    const minting = mount({ kind: "html", shareHref: null });
    expect(statusText(minting.container)).toEqual([p.loading]);
    expect(minting.container.querySelector("iframe")).toBeNull();
    minting.unmount();
    const failed = mount({ kind: "html", shareHref: null, shareFailed: true });
    expect(statusText(failed.container)).toEqual([p.unavailable]);
    failed.unmount();
    const hostLine = mount({ kind: "html", shareHref: null, shareFailed: true, unavailable: "only download" });
    expect(statusText(hostLine.container)).toEqual(["only download"]);
    hostLine.unmount();
    const { container } = mount({
      kind: "html",
      shareHref: "https://studio.test/api/chat/attachment/att-1?sig=s",
      label: "page.html",
    });
    const frame = container.querySelector("iframe")!;
    expect(frame.getAttribute("src")).toBe("https://studio.test/api/chat/attachment/att-1?sig=s");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-popups allow-popups-to-escape-sandbox");
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(frame.className).toBe("md-preview__frame md-preview__frame--fixed");
  });

  it("shows an image at the address the host gives, named by its label", () => {
    const { container } = mount({ kind: "image", src: "/img?token=t", label: "shot.png" });
    const img = container.querySelector("img.md-preview__still")!;
    expect(img.getAttribute("src")).toBe("/img?token=t");
    expect(img.getAttribute("alt")).toBe("shot.png");
  });

  it("frames a PDF at its share link without a sandbox attribute, with 在新頁面顯示 below it when the host has none; with no link coming it shows the host's line", () => {
    const minting = mount({ kind: "pdf", shareHref: null });
    expect(statusText(minting.container)).toEqual([p.loading]);
    expect(minting.container.querySelector("iframe")).toBeNull();
    minting.unmount();
    const unminted = mount({ kind: "pdf", unavailable: "only download" });
    expect(statusText(unminted.container)).toEqual(["only download"]);
    expect(unminted.container.querySelector("iframe")).toBeNull();
    unminted.unmount();
    const failed = mount({ kind: "pdf", shareHref: null, shareFailed: true, unavailable: "only download" });
    expect(statusText(failed.container)).toEqual(["only download"]);
    expect(failed.container.querySelector("iframe")).toBeNull();
    failed.unmount();

    const overlay = mount({ kind: "pdf", shareHref: "https://s/x?sig=1", label: "spec.pdf", fill: true, testId: "body" });
    const frame = overlay.getByTestId("body");
    expect(frame.tagName).toBe("IFRAME");
    expect(frame.getAttribute("src")).toBe("https://s/x?sig=1");
    expect(frame.getAttribute("title")).toBe("spec.pdf");
    expect(frame.hasAttribute("sandbox")).toBe(false);
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(frame.className).toBe("md-preview__frame");
    expect(overlay.container.querySelector("a")).toBeNull();
    overlay.unmount();

    const page = mount({ kind: "pdf", shareHref: "https://s/x?sig=1", newTabLink: true });
    expect(page.container.querySelector("iframe")!.className).toBe("md-preview__frame md-preview__frame--fixed");
    const a = page.container.querySelector("a.md-preview__new-tab")!;
    expect([a.getAttribute("href"), a.getAttribute("target"), a.textContent]).toEqual([
      "https://s/x?sig=1",
      "_blank",
      p.openInNewTab,
    ]);
  });

  it("offers a file it cannot draw a new-tab link in the body when the host asks for one, and the host's line otherwise", () => {
    const header = mount({ kind: "binary", shareHref: "https://s/x?sig=1", unavailable: "only download" });
    expect(statusText(header.container)).toEqual(["only download"]);
    expect(header.container.querySelector("a")).toBeNull();
    header.unmount();
    const inline = mount({ kind: "binary", shareHref: "https://s/x?sig=1", newTabLink: true });
    const a = inline.container.querySelector("a.md-preview__new-tab")!;
    expect(a.getAttribute("href")).toBe("https://s/x?sig=1");
    expect(a.getAttribute("target")).toBe("_blank");
    expect(a.textContent).toBe(p.openInNewTab);
    inline.unmount();
    const none = mount({ kind: "binary", shareHref: null, unavailable: "only download" });
    expect(statusText(none.container)).toEqual(["only download"]);
  });
  it("plays a video at its share link with the browser's own controls, five speeds starting at 1×, and the host's line when the browser cannot play it", () => {
    const { container } = mount({ kind: "video", shareHref: "https://s/v?sig=1", label: "demo.mp4", testId: "body" });
    const video = container.querySelector("video")!;
    expect(video.getAttribute("src")).toBe("https://s/v?sig=1");
    expect(video.getAttribute("aria-label")).toBe("demo.mp4");
    expect(video.hasAttribute("controls")).toBe(true);
    expect(video.hasAttribute("playsinline")).toBe(true);
    expect(video.getAttribute("preload")).toBe("metadata");

    const group = container.querySelector(".md-preview__speed")!;
    expect(group.getAttribute("role")).toBe("group");
    expect(group.getAttribute("aria-label")).toBe(p.playbackSpeed);
    const options = () =>
      Array.from(container.querySelectorAll<HTMLButtonElement>(".md-preview__speed-option")).map(
        (b) => `${b.textContent}:${b.getAttribute("aria-pressed")}`,
      );
    expect(options()).toEqual(["0.5×:false", "1×:true", "1.25×:false", "1.5×:false", "2×:false"]);
    expect([video.playbackRate, video.defaultPlaybackRate]).toEqual([1, 1]);

    fireEvent.click(container.querySelectorAll(".md-preview__speed-option")[2]!);
    expect(options()).toEqual(["0.5×:false", "1×:false", "1.25×:true", "1.5×:false", "2×:false"]);
    expect([video.playbackRate, video.defaultPlaybackRate]).toEqual([1.25, 1.25]);

    fireEvent.error(video);
    expect(container.querySelector("video")).toBeNull();
    expect(statusText(container)).toEqual([p.unavailable]);
  });

  it("follows a speed picked in the browser's own controls", () => {
    const { container } = mount({ kind: "video", shareHref: "https://s/v?sig=1" });
    const video = container.querySelector("video")!;
    video.playbackRate = 1.5;
    fireEvent(video, new Event("ratechange"));
    expect(container.querySelector(".md-preview__speed-option--on")!.textContent).toBe("1.5×");
    video.playbackRate = 1.75;
    fireEvent(video, new Event("ratechange"));
    expect(container.querySelector(".md-preview__speed-option--on")).toBeNull();
  });

  it("starts a new video at 1× and playable again", () => {
    const view = mount({ kind: "video", shareHref: "/a.mp4?sig=1", unavailable: "only download" });
    fireEvent.click(view.container.querySelectorAll(".md-preview__speed-option")[4]!);
    expect(view.container.querySelector("video")!.playbackRate).toBe(2);
    fireEvent.error(view.container.querySelector("video")!);
    expect(statusText(view.container)).toEqual(["only download"]);
    view.rerender(
      <I18nProvider>
        <div data-testid="host">
          <ContentView kind="video" shareHref="/b.mp4?sig=2" unavailable="only download" />
        </div>
      </I18nProvider>,
    );
    const video = view.container.querySelector("video")!;
    expect(video.getAttribute("src")).toBe("/b.mp4?sig=2");
    expect([video.playbackRate, video.defaultPlaybackRate]).toEqual([1, 1]);
    expect(view.container.querySelector(".md-preview__speed-option--on")!.textContent).toBe("1×");
  });

  it("says loading while the video's link is being made, and the host's line when no link is coming", () => {
    const minting = mount({ kind: "video", shareHref: null, src: "/v?token=t" });
    expect(minting.container.querySelector("video")).toBeNull();
    expect(statusText(minting.container)).toEqual([p.loading]);
    minting.unmount();
    const failed = mount({ kind: "video", shareHref: null, shareFailed: true, unavailable: "only download" });
    expect(failed.container.querySelector("video")).toBeNull();
    expect(statusText(failed.container)).toEqual(["only download"]);
    failed.unmount();
    const none = mount({ kind: "video", unavailable: "only download" });
    expect(statusText(none.container)).toEqual(["only download"]);
  });
});
