// The row set for HostRelativeLinkStory, in a module of its own.
// Playwright's component-test transform rewrites the imports a mount()
// touches, so a module that exports the component CANNOT also export the
// data the spec reads — the generated registry declares the name twice and
// the build fails before any test runs.
/** One row per shape, so a single mount decides every case and a failure names
 * which shape broke. */
export const ROWS: { id: string; source: string }[] = [
  { id: "task", source: "[T-1](/#tasks/T-1)" },
  { id: "card", source: "[card](/#replies/card/rc-1)" },
  { id: "protocol-relative", source: "[safe?](//evil.example/x)" },
  { id: "backslash", source: "[safe?](/\\evil.example/x)" },
  { id: "tab", source: "[safe?](/\t/evil.example)" },
  { id: "absolute", source: "[out](https://evil.example/x)" },
];

/** The raw link TARGET of each row, spelled here rather than scraped back out
 * of the DOM: `innerText` normalises a TAB to a space, which turns the one
 * shape whose danger IS the TAB into a harmless one and quietly makes the
 * measurement agree with itself. */
export const TARGETS: Record<string, string> = {
  task: "/#tasks/T-1",
  card: "/#replies/card/rc-1",
  "protocol-relative": "//evil.example/x",
  backslash: "/\\evil.example/x",
  tab: "/\t/evil.example",
  absolute: "https://evil.example/x",
};
