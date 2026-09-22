// CT story for the host-free link class. It mounts the REAL Markdown renderer
// on the four target shapes at once, so one page load decides all of them, and
// gives each row a stable id to locate by.
//
// The point of doing this in a browser rather than jsdom: what a path RESOLVES
// to is the browser's job, not the renderer's. jsdom can only tell us the
// attribute we wrote; Chromium tells us the address a click would actually go
// to, which is the whole claim this change makes.
import { Markdown } from "../../src/components/Markdown";

import { ROWS } from "./hostRelativeLinkRows";

export function HostRelativeLinkStory() {
  return (
    <div data-surface="host-relative-link">
      {ROWS.map((r) => (
        <div key={r.id} data-row={r.id}>
          <Markdown source={r.source} />
        </div>
      ))}
    </div>
  );
}
