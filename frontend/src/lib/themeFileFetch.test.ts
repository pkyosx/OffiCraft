import { describe, expect, it, vi } from "vitest";
import { themeFileLoaders } from "./themeFileFetch";

describe("themeFileLoaders", () => {
  const bundle = { id: "harvest", name: "Harvest", colors: { "--color-bg": "#15101f" } };

  it("requests nothing until a loader is called, then resolves that file's parsed bundle", async () => {
    const request = vi.fn(async () => new Response(JSON.stringify(bundle), { status: 200 }));
    const loaders = themeFileLoaders(
      {
        "../../../themes/harvest.theme.json": "/assets/harvest.theme-abc.json",
        "../../../themes/frost.theme.json": "/assets/frost.theme-def.json",
      },
      request
    );
    expect(request).not.toHaveBeenCalled();

    await expect(loaders["../../../themes/harvest.theme.json"]()).resolves.toEqual(bundle);
    expect(request.mock.calls).toEqual([["/assets/harvest.theme-abc.json"]]);
  });

  it("rejects with the status when the file answers non-OK, and a later call requests it again", async () => {
    const request = vi
      .fn<(url: string) => Promise<Response>>()
      .mockResolvedValueOnce(new Response("gone", { status: 503 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(bundle), { status: 200 }));
    const load = themeFileLoaders(
      { "../../../themes/harvest.theme.json": "/assets/harvest.theme-abc.json" },
      request
    )["../../../themes/harvest.theme.json"];

    await expect(load()).rejects.toThrow("theme file /assets/harvest.theme-abc.json: HTTP 503");
    await expect(load()).resolves.toEqual(bundle);
    expect(request).toHaveBeenCalledTimes(2);
  });
});
