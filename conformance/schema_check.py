"""Minimal OpenAPI-schema conformance checker — pure stdlib, black-box safe.

Validates a decoded JSON instance against the subset
of JSON Schema that ``spec/openapi.json`` actually uses: ``$ref`` into
``#/components/schemas``, ``type`` (incl. the 3.1 list form), ``required`` /
``properties`` / ``additionalProperties``, ``items``, ``enum``, and
``anyOf``/``oneOf``/``allOf`` (pydantic spells nullable as
``anyOf: [{type: X}, {type: "null"}]``).

Deliberately NOT a full JSON Schema engine: unknown keywords are ignored, an
empty/missing schema constrains nothing (same permissive semantics as the
spec). Returns a list of human-readable violations — empty means conformant.
"""

from __future__ import annotations

from typing import Any


def _resolve_ref(ref: str, spec: dict[str, Any]) -> dict[str, Any]:
    assert ref.startswith("#/"), f"only intra-document refs supported: {ref}"
    node: Any = spec
    for part in ref[2:].split("/"):
        node = node[part]
    return node


def _type_ok(instance: Any, typ: str) -> bool:
    if typ == "null":
        return instance is None
    if typ == "boolean":
        return isinstance(instance, bool)
    if typ == "integer":
        return isinstance(instance, int) and not isinstance(instance, bool)
    if typ == "number":
        return isinstance(instance, (int, float)) and not isinstance(instance, bool)
    if typ == "string":
        return isinstance(instance, str)
    if typ == "array":
        return isinstance(instance, list)
    if typ == "object":
        return isinstance(instance, dict)
    return True  # unknown type keyword: constrain nothing


def violations(
    instance: Any,
    schema: Any,
    spec: dict[str, Any],
    where: str = "$",
) -> list[str]:
    """All the ways ``instance`` fails ``schema`` (resolved against ``spec``)."""
    out: list[str] = []
    if not isinstance(schema, dict) or not schema:
        return out
    if "$ref" in schema:
        return violations(instance, _resolve_ref(schema["$ref"], spec), spec, where)

    if "allOf" in schema:
        for sub in schema["allOf"]:
            out.extend(violations(instance, sub, spec, where))
    for combinator in ("anyOf", "oneOf"):
        if combinator in schema:
            branches = [
                violations(instance, sub, spec, where) for sub in schema[combinator]
            ]
            if not any(len(b) == 0 for b in branches):
                flat = "; ".join(v for b in branches for v in b)
                out.append(f"{where}: matches no {combinator} branch ({flat})")

    if "enum" in schema and instance not in schema["enum"]:
        out.append(f"{where}: {instance!r} not in enum {schema['enum']!r}")

    typ = schema.get("type")
    if typ is not None:
        types = typ if isinstance(typ, list) else [typ]
        if not any(_type_ok(instance, t) for t in types):
            out.append(
                f"{where}: expected type {typ!r}, got {type(instance).__name__}"
            )
            return out  # structural keywords below assume the right type

    if isinstance(instance, dict):
        for req in schema.get("required", []):
            if req not in instance:
                out.append(f"{where}.{req}: required property missing")
        props = schema.get("properties", {})
        extra = schema.get("additionalProperties")
        for key, value in instance.items():
            if key in props:
                out.extend(violations(value, props[key], spec, f"{where}.{key}"))
            elif isinstance(extra, dict):
                out.extend(violations(value, extra, spec, f"{where}.{key}"))

    if isinstance(instance, list) and "items" in schema:
        for i, item in enumerate(instance):
            out.extend(violations(item, schema["items"], spec, f"{where}[{i}]"))

    return out
