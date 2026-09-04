#!/usr/bin/env python3
"""chat-pushdown-guard — the chat listing and the unread count stay pushed down
into SQL, and unread counting keeps exactly one entry point.

WHAT IT ENFORCES, both measured from the tree on every run:

  (1) `HandleListChatApiChatGet` does not call `s.dal.ListChat()` (the whole
      chat_message table) and does not write a read receipt. The cursorless page
      is `s.dal.ListChatLatest`; GET /api/chat has had no watermark side effect
      since T-48 (owner ruling c-d1eea83e57d1).

  (2) `s.dal.UnreadCountsFor` is called from EXACTLY ONE place in production
      code — `apiServer.unreadCountsForRequest` in api_helpers.go — and the pure
      whole-stream fold `UnreadCounts` is called from NOWHERE in production code.

(2)'s second half is the first half's back door: a new surface could honour "one
caller of the DAL method" and simply re-fold in Go instead, which is exactly the
shape the five original copies had. `UnreadCounts` takes the entire chat stream
as an argument, so a call to it IS a full-table read.

WHY AN ENTRY POINT RATHER THAN A LIST OF APPROVED SITES: the first version of
this rule named the two call sites T-48 was briefed on. Three more already
existed in api_outsource.go and it said nothing about them, because a guard that
enumerates cannot see what it was not told. One door is checkable; five named
sites are a list that goes stale the moment somebody adds a sixth.

🔴 WHAT THIS DOES **NOT** SAY: that the surfaces behave alike. They must not.
The red dot filters removed members and released workers before summing, the
roster binds one number per member, the contractor faces index by worker id.
Those differences live in their own handlers on purpose — this rule is about
where the ALGORITHM lives, not about what each caller does with the answer.

🔴 WHY IT IS A LINT AND NOT A GO TEST (T-48, R13-6). Both rules used to be
`_test.go` functions in `server/ocserverd/dal_chat_pushdown_t48_test.go`, and
both worked by `os.ReadFile`-ing production `.go` files and matching strings.
The thirteenth review's judgement, which is right: extracting the handler's body
into a helper changes no behaviour at all and flips them from green to red (or
back), so they are not behaviour tests — they are lint rules living in the wrong
house. This repo already has that house.

⚠️ WHAT WAS NOT MOVED, and why the file they came from is still worth reading:
its OTHER tests (`TestListChatLatestMatchesTheGoFilterItReplaced`,
`TestUnreadCountsForMatchesTheGoFold`, …) keep the pre-pushdown Go implementation
as an ORACLE and compare full results over the same fixture. Those assert
outputs, not source text, and they stay where they are. This guard cannot
replace them: it says the fast path is the only path, they say the fast path
gives the same answers.

⚠️ THE HOLE, said out loud: this is a text scan. A caller reached through a
function pointer, an interface method or a differently-spelled alias is invisible
to it, and so is a hand-written fold that never names `UnreadCounts`. Closing
that needs `apiServer.dal` to become an interface a counting fake can be slid
into — a real seam, deliberately not opened here.

Run: python3 bin/chat-pushdown-guard.py
"""

import os
import re
import sys

ROOT = os.environ.get("CHAT_PUSHDOWN_ROOT") or os.path.dirname(
    os.path.dirname(os.path.abspath(__file__))
)
SKIP_DIRS = {".git", "node_modules", "vendor", "dist", "var"}
HANDLER = "func (s *apiServer) HandleListChatApiChatGet("
ENTRY_POINT = "func (s *apiServer) unreadCountsForRequest("
API_CHAT = os.path.join("server", "ocserverd", "api_chat.go")
API_HELPERS = os.path.join("server", "ocserverd", "api_helpers.go")


def production_go_files(root):
    """Every non-test .go file under root, as (relative path, source)."""
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in sorted(filenames):
            if not name.endswith(".go") or name.endswith("_test.go"):
                continue
            path = os.path.join(dirpath, name)
            with open(path, encoding="utf-8") as fh:
                yield os.path.relpath(path, root), fh.read()


def strip_comment(line):
    """A comment may DISCUSS either call; only code counts."""
    i = line.find("//")
    return line if i < 0 else line[:i]


def handler_body(src):
    """The text of HandleListChatApiChatGet, or None when it is not there."""
    start = src.find(HANDLER)
    if start < 0:
        return None
    body = src[start:]
    end = body.find("\n}\n")
    return body[:end] if end > 0 else body


def main():
    problems = []

    # ── rule 1: the cursorless chat page ─────────────────────────────────────
    api_chat_path = os.path.join(ROOT, API_CHAT)
    if not os.path.exists(api_chat_path):
        problems.append(f"{API_CHAT} is missing — this guard cannot prove anything")
    else:
        with open(api_chat_path, encoding="utf-8") as fh:
            body = handler_body(fh.read())
        if body is None:
            problems.append(
                "cannot find HandleListChatApiChatGet in "
                f"{API_CHAT} — the guard has gone stale, point it at the new name"
            )
        else:
            code = "\n".join(strip_comment(l) for l in body.split("\n"))
            if "s.dal.ListChat()" in code:
                problems.append(
                    "HandleListChatApiChatGet reads the WHOLE chat_message table "
                    "again; the cursorless page is s.dal.ListChatLatest (T-48)"
                )
            if "s.dal.PutChatRead(" in code:
                problems.append(
                    "HandleListChatApiChatGet writes a read receipt again — "
                    "GET /api/chat has no watermark side effect "
                    "(T-48, c-d1eea83e57d1)"
                )

    # ── rule 2: one entry point for unread counting ──────────────────────────
    dal_callers, go_folders = [], []
    scanned = 0
    for rel, src in production_go_files(ROOT):
        scanned += 1
        # The enclosing func is tracked so the ONE legal caller can be named by
        # the function it lives in rather than by a line number that drifts.
        enclosing = ""
        for i, line in enumerate(src.split("\n"), start=1):
            if line.startswith("func "):
                enclosing = line
            code = strip_comment(line)
            where = f"{rel}:{i}: {line.strip()}"
            if "UnreadCountsFor(" in code and "func (d *DAL) UnreadCountsFor(" not in code:
                if not enclosing.startswith(ENTRY_POINT):
                    dal_callers.append(where)
            if re.search(r"(?<![A-Za-z0-9_])UnreadCounts\(", code) and "func UnreadCounts(" not in code:
                go_folders.append(where)

    if scanned < 50:
        problems.append(
            f"the walk only saw {scanned} production .go files — the root is "
            "wrong and this guard proves nothing"
        )
    if dal_callers:
        problems.append(
            "unread counting has more than one entry point: these reach "
            "s.dal.UnreadCountsFor directly instead of going through "
            "apiServer.unreadCountsForRequest (T-48) —\n    "
            + "\n    ".join(dal_callers)
        )
    if go_folders:
        problems.append(
            "production code still folds unread counts in Go (UnreadCounts takes "
            "the WHOLE chat stream, so each of these is a full-table read; go "
            "through apiServer.unreadCountsForRequest — T-48):\n    "
            + "\n    ".join(go_folders)
        )

    # The scan must actually FIND the legal caller, or an entry point that was
    # renamed or deleted would leave this guard green over nothing at all.
    helpers_path = os.path.join(ROOT, API_HELPERS)
    if not os.path.exists(helpers_path):
        problems.append(f"{API_HELPERS} is missing — the entry point cannot be checked")
    else:
        with open(helpers_path, encoding="utf-8") as fh:
            helpers = fh.read()
        i = helpers.find(ENTRY_POINT)
        if i < 0:
            problems.append(
                "apiServer.unreadCountsForRequest is gone — the entry point this "
                "guard names no longer exists"
            )
        elif "s.dal.UnreadCountsFor(" not in helpers[i : i + 400]:
            problems.append(
                "apiServer.unreadCountsForRequest no longer calls "
                "s.dal.UnreadCountsFor — the rule above would now be vacuously green"
            )

    if problems:
        print("[chat-pushdown] FAIL", file=sys.stderr)
        for p in problems:
            print(f"  {p}", file=sys.stderr)
        return 1

    print(
        f"[chat-pushdown] ok — {scanned} production .go files scanned; the chat "
        "page stays pushed down and unread counting has one entry point"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
