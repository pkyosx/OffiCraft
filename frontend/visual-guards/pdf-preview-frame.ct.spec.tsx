// A PDF drawn in the preview's frame, in the full Chromium: the default
// headless shell has no PDF viewer and downloads every PDF, so it cannot tell a
// drawn PDF from a blocked one. The frame must carry no sandbox attribute —
// Chromium blocks a PDF in a sandboxed frame — and the route answers with the
// headers the serve route sends for a sig-only PDF.
import { test, expect } from "@playwright/experimental-ct-react";
import { I18nProvider } from "../src/i18n";
import { MarkdownPreviewOverlay } from "../src/components/MarkdownPreviewOverlay";

test.use({ channel: "chromium", launchOptions: { args: ["--use-mock-keychain"] } });

// The bytes the serve route would send, with a valid xref so the viewer has
// nothing to repair.
function pdfBytes(text: string): string {
  const stream = `BT /F1 48 Tf 72 700 Td (${text}) Tj ET`;
  const objs = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let out = "%PDF-1.4\n";
  const offsets = objs.map((o, i) => {
    const at = out.length;
    out += `${i + 1} 0 obj\n${o}\nendobj\n`;
    return at;
  });
  const xref = out.length;
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`;
  out += offsets.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("");
  out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return out;
}

test("pdf: the share link draws in the body's frame under the headers the server sends", async ({
  mount,
  page,
}) => {
  await page.route("**/api/chat/attachment/att-pdf?sig=*", (route) =>
    route.fulfill({
      status: 200,
      headers: {
        "Content-Type": "application/pdf",
        "Content-Disposition": `inline; filename="spec.pdf"; filename*=UTF-8''spec.pdf`,
        "Content-Security-Policy": "sandbox",
        "X-Content-Type-Options": "nosniff",
      },
      body: pdfBytes("Hello PDF"),
    }),
  );
  await page.setViewportSize({ width: 1100, height: 760 });
  await mount(
    <I18nProvider>
      <MarkdownPreviewOverlay
        title="spec.pdf"
        url="/api/chat/attachment/att-pdf"
        attachmentId="att-pdf"
        mime="application/pdf"
        onClose={() => {}}
      />
    </I18nProvider>,
  );
  const dialog = page.getByRole("dialog", { name: "spec.pdf" });
  await expect(dialog.getByRole("link", { name: "在新頁面顯示" })).toBeVisible();
  await expect(dialog.getByRole("link", { name: "下載" })).toBeVisible();
  const frame = page.locator("iframe.md-preview__frame");
  await expect(frame).toBeVisible();
  const box = (await frame.boundingBox())!;
  expect(box.height, "the frame fills the body").toBeGreaterThan(400);
  await expect
    .poll(
      async () => {
        const doc = await frame.elementHandle().then((el) => el!.contentFrame());
        const viewer = doc?.childFrames()[0];
        if (!viewer) return doc?.url() ?? "no frame";
        return viewer.evaluate(() => (document.querySelector("pdf-viewer#viewer") ? "viewer" : "empty"));
      },
      { message: "Chromium's PDF viewer loads inside the frame", timeout: 10_000 },
    )
    .toBe("viewer");
});
