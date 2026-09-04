#!/usr/bin/env python3
"""Positive AND negative control for bin/chat-pushdown-guard.py.

The guard replaces two Go tests that used to read production source text
(T-48, R13-6). Moving a rule to a new house is only honest if the new house
still reddens, so every violation it claims to catch is replanted here in a
temp copy of the tree and must produce a NAMED failure — and the shipped tree
must stay green.

FOUR CLASSES, and a future edit must keep all four:

  * SHIPPED   — the real tree, unmodified: green. The false-positive control.
  * VIOLATION — each rule broken in isolation: red, and the message must name
                what broke ("went red" alone is satisfied by a guard reddening
                for an unrelated reason).
  * VACUOUS   — the two ways the guard can be silently disarmed (the handler
                renamed away, the entry point renamed away): red, because a
                guard that cannot find what it checks proves nothing.
  * COMMENT   — the same violations written inside a `//` comment: GREEN. A
                sentence discussing the old shape is not the old shape, and a
                guard that reddens on prose gets deleted by the next person.

Run: python3 bin/tests/chat-pushdown-guard-selftest.py
"""

import os
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
GUARD = os.path.join(ROOT, "bin", "chat-pushdown-guard.py")
API_CHAT = os.path.join("server", "ocserverd", "api_chat.go")
API_HELPERS = os.path.join("server", "ocserverd", "api_helpers.go")
API_OUTSOURCE = os.path.join("server", "ocserverd", "api_outsource.go")

failures = []


def run(root):
    p = subprocess.run(
        [sys.executable, GUARD],
        capture_output=True,
        text=True,
        env={**os.environ, "CHAT_PUSHDOWN_ROOT": root},
    )
    return p.returncode, p.stdout + p.stderr


def with_tree(edit=None):
    """A copy of the repo's Go sources, optionally sabotaged, and the verdict."""
    tmp = tempfile.mkdtemp(prefix="chat-pushdown-")
    try:
        dst = os.path.join(tmp, "repo")
        shutil.copytree(
            ROOT,
            dst,
            ignore=shutil.ignore_patterns(
                ".git", "node_modules", "vendor", "dist", "var", "work", "*.db"
            ),
            symlinks=True,
        )

        def patch(rel, fn):
            path = os.path.join(dst, rel)
            with open(path, encoding="utf-8") as fh:
                src = fh.read()
            new = fn(src)
            assert new != src, f"sabotage of {rel} changed nothing — it is stale"
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(new)

        if edit:
            edit(patch)
        return run(dst)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def expect(name, cond, detail=""):
    if cond:
        print(f"  ok   {name}")
    else:
        print(f"  FAIL {name}{(': ' + detail) if detail else ''}")
        failures.append(name)


def insert_in_handler(src, line):
    """Put `line` inside HandleListChatApiChatGet's body."""
    marker = "func (s *apiServer) HandleListChatApiChatGet("
    i = src.index(marker)
    j = src.index("\n", src.index("{", i))
    return src[:j] + "\n\t" + line + src[j:]


print("[chat-pushdown-selftest] SHIPPED")
code, out = with_tree()
expect("the tree as shipped is green", code == 0 and "[chat-pushdown] ok" in out, out)

print("[chat-pushdown-selftest] VIOLATION")
code, out = with_tree(
    lambda patch: patch(
        API_CHAT, lambda s: insert_in_handler(s, "_, _ = s.dal.ListChat()")
    )
)
expect(
    "a whole-table read in the chat handler is named",
    code != 0 and "WHOLE chat_message table" in out,
    out,
)

code, out = with_tree(
    lambda patch: patch(
        API_CHAT,
        lambda s: insert_in_handler(s, '_ = s.dal.PutChatRead("", "", 0)'),
    )
)
expect(
    "a read receipt written by GET /api/chat is named",
    code != 0 and "watermark side effect" in out,
    out,
)

code, out = with_tree(
    lambda patch: patch(
        API_OUTSOURCE,
        lambda s: s.replace(
            "package main",
            "package main\n\nfunc secondDoor(s *apiServer) { _, _ = s.dal.UnreadCountsFor(\"\", nil) }",
            1,
        ),
    )
)
expect(
    "a second caller of s.dal.UnreadCountsFor is named with its file",
    code != 0 and "more than one entry point" in out and "api_outsource.go" in out,
    out,
)

code, out = with_tree(
    lambda patch: patch(
        API_OUTSOURCE,
        lambda s: s.replace(
            "package main",
            "package main\n\nfunc backDoor() { _ = UnreadCounts(nil, nil, \"\") }",
            1,
        ),
    )
)
expect(
    "a Go re-fold of the whole stream is named with its file",
    code != 0 and "folds unread counts in Go" in out and "api_outsource.go" in out,
    out,
)

print("[chat-pushdown-selftest] VACUOUS")
code, out = with_tree(
    lambda patch: patch(
        API_CHAT,
        lambda s: s.replace(
            "func (s *apiServer) HandleListChatApiChatGet(",
            "func (s *apiServer) HandleListChatApiChatGetRenamed(",
        ),
    )
)
expect(
    "a renamed chat handler is refused rather than passed over",
    code != 0 and "gone stale" in out,
    out,
)

code, out = with_tree(
    lambda patch: patch(
        API_HELPERS,
        lambda s: s.replace(
            "func (s *apiServer) unreadCountsForRequest(",
            "func (s *apiServer) unreadCountsForRequestRenamed(",
        ),
    )
)
expect(
    "a renamed entry point is refused rather than leaving the rule vacuous",
    code != 0 and "entry point" in out,
    out,
)

print("[chat-pushdown-selftest] COMMENT")
code, out = with_tree(
    lambda patch: patch(
        API_CHAT,
        lambda s: insert_in_handler(
            s, "// once upon a time this called s.dal.ListChat() and s.dal.PutChatRead("
        ),
    )
)
expect(
    "prose about the old shape is not the old shape",
    code == 0 and "[chat-pushdown] ok" in out,
    out,
)

if failures:
    print(f"\n[chat-pushdown-selftest] FAILED: {', '.join(failures)}")
    sys.exit(1)
print("\n[chat-pushdown-selftest] all controls behaved")
