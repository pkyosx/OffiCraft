#!/usr/bin/env python3
"""Add parameter descriptions into spec/openapi.json's x-mcp legacy descriptors.

The descriptors are JSON *strings*, and the file's escaping convention is not
even consistent inside one descriptor: get_chat carries a tool-level
description with an escaped arrow and parameter descriptions with a literal
one. Re-serializing therefore cannot preserve the file, so nothing here is
re-serialized. The new key is spliced into the raw descriptor text at the
position alphabetical key order puts it, and every other byte is left alone.

The splice is checked by parsing before and after: the only difference the two
objects may show is exactly the descriptions being added.
"""
import json
import re
import sys

SPEC = "spec/openapi.json"


def _match_brace(text, i):
    """Index just past the `{` at text[i]'s matching `}` — string-aware."""
    depth, j = 0, i
    while j < len(text):
        c = text[j]
        if c == '"':
            j += 1
            while text[j] != '"':
                j += 2 if text[j] == "\\" else 1
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return j
        j += 1
    raise ValueError("unbalanced braces")


def _top_keys(text, lo, hi):
    """(key, index) for each key directly inside the object spanning lo..hi."""
    out, depth, j = [], 0, lo
    while j < hi:
        c = text[j]
        if c == '"':
            k = j + 1
            while text[k] != '"':
                k += 2 if text[k] == "\\" else 1
            if depth == 1 and text[k + 1:k + 2] == ":":
                out.append((json.loads(text[j:k + 1]), j))
            j = k
        elif c == "{" or c == "[":
            depth += 1
        elif c == "}" or c == "]":
            depth -= 1
        j += 1
    return out


def _string_span(text, i):
    """(start, end) of the JSON string literal beginning at text[i] == '"'."""
    j = i + 1
    while text[j] != '"':
        j += 2 if text[j] == "\\" else 1
    return i, j + 1


def splice(raw, param, desc):
    """Insert a description key into `param`'s object inside the raw descriptor.

    Indentation is NOT uniform in these descriptors — hand edits have left a
    property whose opening brace, keys and closing brace all sit at different
    depths — so nothing here is derived from indentation. The object is found
    by brace matching, and the inserted line copies the indentation of the
    neighbouring key it is placed against.
    """
    encoded = json.dumps(desc)
    opener = re.compile(r'^(\s*)"' + re.escape(param) + r'": \{', re.M)
    hits = list(opener.finditer(raw))
    if len(hits) != 1:
        sys.exit(f"{param}: property object is not uniquely locatable "
                 f"({len(hits)} candidates)")
    brace = raw.index("{", hits[0].start())
    close = _match_brace(raw, brace)

    keys = _top_keys(raw, brace, close)
    # An existing description is REPLACED IN PLACE — only the encoded string
    # value is swapped, so the key's position, the surrounding indentation and
    # every neighbouring byte survive untouched. Rewriting a wrong sentence and
    # adding a missing one are the same surgery on the same object; doing the
    # rewrite as delete+insert would move the key to alphabetical position and
    # show up as a spurious diff on a line nobody meant to touch.
    existing = [pos for key, pos in keys if key == "description"]
    if existing:
        colon = raw.index(":", existing[0])
        vstart = colon + 1
        while raw[vstart] in " \t\n":
            vstart += 1
        if raw[vstart] != '"':
            sys.exit(f"{param}: existing description is not a plain string")
        lo, hi = _string_span(raw, vstart)
        return raw[:lo] + encoded + raw[hi:]

    after = [pos for key, pos in keys if key > "description"]
    anchor = after[0] if after else (keys[-1][1] if keys else None)
    if anchor is None:  # empty object: `{}`
        return raw[:brace + 1] + f'"description": {encoded}' + raw[close:]

    line_start = raw.rfind("\n", 0, anchor) + 1
    indent = raw[line_start:anchor]
    # An inline property (`"key": {"type": "string"}`) puts its keys mid-line,
    # so there is no indentation to copy and no new line to open: splice the
    # pair in beside its neighbour instead.
    if indent.strip():
        if after:
            return raw[:anchor] + f'"description": {encoded}, ' + raw[anchor:]
        tail = raw[:close].rstrip()
        return raw[:len(tail)] + f', "description": {encoded}' + raw[len(tail):]
    if after:
        return raw[:line_start] + f'{indent}"description": {encoded},\n' + raw[line_start:]
    # Appended last. The insertion point is the end of the LAST VALUE, not the
    # end of the last key's line — a value can span many lines (an object, a
    # multi-line anyOf), and splitting it mid-value produces invalid JSON.
    tail = raw[:close].rstrip()
    return raw[:len(tail)] + f',\n{indent}"description": {encoded}' + raw[len(tail):]


def load():
    with open(SPEC, encoding="utf-8") as f:
        return f.read()


def tools(spec):
    """Yield (raw_descriptor, parsed, tool_name)."""
    for ops in spec["paths"].values():
        for op in ops.values():
            if not isinstance(op, dict):
                continue
            x = op.get("x-mcp")
            if not x:
                continue
            raw = x.get("legacy", {}).get("descriptor")
            if raw is None:
                continue
            yield raw, json.loads(raw), x.get("name")


def apply(descs, overwrite=False):
    """descs: {tool: {param: description}} -> writes SPEC in place."""
    text = load()
    spec = json.loads(text)
    by_tool = {name: (raw, obj) for raw, obj, name in tools(spec)}

    unknown_tools = sorted(set(descs) - set(by_tool))
    if unknown_tools:
        sys.exit(f"unknown tools: {unknown_tools}")

    touched = 0
    for tool, params in descs.items():
        raw, obj = by_tool[tool]
        props = obj["inputSchema"]["properties"]
        missing = sorted(set(params) - set(props))
        if missing:
            sys.exit(f"{tool}: no such parameters: {missing}")

        new_raw = raw
        for param, desc in params.items():
            if props[param].get("description") and not overwrite:
                sys.exit(f"{tool}.{param}: already has a description; refusing to overwrite")
            new_raw = splice(new_raw, param, desc)

        # The splice is text surgery on a JSON blob: prove it changed exactly
        # what it was asked to and nothing else. The baseline drops the SAME
        # keys the check pops off the result, so an overwrite is held to the
        # identical standard an insert is: after removing this batch's
        # descriptions, the two objects must be equal. With nothing there
        # before, `expected` is the untouched original, i.e. exactly the
        # insert-only assertion this replaced.
        expected = json.loads(raw)
        for param in params:
            expected["inputSchema"]["properties"][param].pop("description", None)
        after = json.loads(new_raw)
        for param, desc in params.items():
            got = after["inputSchema"]["properties"][param].pop("description", None)
            if got != desc:
                sys.exit(f"{tool}.{param}: splice did not land the description")
        if after != expected:
            sys.exit(f"{tool}: splice changed something other than the descriptions")

        old_json = json.dumps(raw, ensure_ascii=False)
        new_json = json.dumps(new_raw, ensure_ascii=False)
        if text.count(old_json) != 1:
            sys.exit(f"{tool}: descriptor string is not uniquely locatable in the file")
        text = text.replace(old_json, new_json)
        touched += 1

    with open(SPEC, "w", encoding="utf-8") as f:
        f.write(text)
    return touched


def audit():
    """Print totals: params, described, blank."""
    spec = json.loads(load())
    total = described = 0
    for _, obj, _ in tools(spec):
        for p in obj["inputSchema"].get("properties", {}).values():
            total += 1
            if p.get("description"):
                described += 1
    print(f"params={total} described={described} blank={total - described}")


if __name__ == "__main__":
    argv = sys.argv[1:]
    if argv == ["audit"]:
        audit()
    else:
        overwrite = "--overwrite" in argv
        rest = [a for a in argv if a != "--overwrite"]
        payload = json.load(open(rest[0], encoding="utf-8"))
        print(f"rewrote {apply(payload, overwrite)} descriptors")
