#!/usr/bin/env python3
"""Seed one identical corpus into a live ocserverd, then measure three answers.

    measure.py BASE_URL OWNER_PASSWORD > arm.json

Character counts are Unicode CODE POINTS: the body is decoded as UTF-8 into a
str and ``len()`` is taken.

⚠️ NOT ``wc -m``. This host has no LANG set, so ``wc -m`` counts BYTES — which
for the CJK corpus below over-reports by roughly 3×, in the direction that
flatters the change. The unit has to be the same one the server's own caps are
expressed in (code points), or the "before" number is not comparable to the cap
it was supposedly filling.

The corpus is filled to 90% of the cap the SERVER reports for each capped
document. That is deliberately close to the WORST case: the whole point of the
change is what a list answer costs when its documents are long, and a cap is
where a long-lived document ends up. Do not read the resulting ratios as
typical load — read them as the ceiling the old shape had.
"""
import json
import sys
import urllib.error
import urllib.request

READS = 3  # per endpoint, so float-repr jitter shows up instead of hiding


def make_req(base):
    def req(method, path, body=None, token=None):
        data = json.dumps(body).encode() if body is not None else None
        r = urllib.request.Request(base + path, data=data, method=method)
        r.add_header("Content-Type", "application/json")
        if token:
            r.add_header("Authorization", "Bearer " + token)
        try:
            with urllib.request.urlopen(r) as resp:
                return resp.status, resp.read().decode("utf-8")
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode("utf-8")
    return req


def filled(unit, n):
    return (unit * ((n // len(unit)) + 1))[:n]


def main(argv):
    if len(argv) != 3:
        print("usage: measure.py BASE_URL OWNER_PASSWORD", file=sys.stderr)
        return 64
    base, password = argv[1], argv[2]
    req = make_req(base)

    st, body = req("POST", "/api/login", {"password": password})
    assert st == 200, ("login", st, body[:300])
    tok = json.loads(body)["token"]

    caps = json.loads(req("GET", "/api/settings", None, tok)[1])
    caps = {k: v for k, v in caps.items() if k.startswith("doc_cap_chars")}

    SOP = filled("這是一份任務手冊的標準作業流程。每一步都要寫清楚為什麼,而不只是怎麼做。\n",
                 int(caps["doc_cap_chars_manual_sop"] * 0.9))
    LEARN = filled("結案回寫的學習經驗:這一類任務最常見的失敗是把預設值當成無害的選項。\n",
                   int(caps["doc_cap_chars_manual_learnings"] * 0.9))
    DEF = filled("這個角色的職責定義。它負責的事、它不負責的事,以及兩者的界線在哪裡。\n",
                 int(caps["doc_cap_chars_duty"] * 0.9))
    DOC = filled("全域脈絡的一段內容,長度刻意接近真實文件,好讓字數量測不是玩具尺度。\n", 3000)

    for i in range(3):
        tk = "conf-type-%d" % i
        st, b = req("POST", "/api/task-manuals", {"type_key": tk}, tok)
        assert st in (200, 201), ("create manual", st, b[:300])
        st, b = req("POST", "/api/task-manuals/" + tk,
                    {"display_name": "手冊 %d" % i, "purpose": "用途 %d" % i,
                     "sop_md": SOP, "learnings": LEARN}, tok)
        assert st == 200, ("fill manual", st, b[:300])

    for i in range(3):
        # The server mints the role key — never client-supplied.
        st, b = req("POST", "/api/roles", {"name": "角色 %d" % i}, tok)
        assert st in (200, 201), ("create role", st, b[:300])
        rk = json.loads(b)["role"]["key"]
        st, b = req("POST", "/api/roles/" + rk, {"definition_md": DEF}, tok)
        assert st == 200, ("fill role", st, b[:300])

    for i in range(4):  # 4 saves -> 3 retained revisions (the cap)
        st, b = req("POST", "/api/global-context",
                    {"text": DOC + str(i), "allow_shrink": True}, tok)
        assert st == 200, ("save global context", st, b[:300])

    out = {
        "_caps": caps,
        "_corpus_chars": {"sop_md": len(SOP), "learnings": len(LEARN),
                          "definition_md": len(DEF), "global_context": len(DOC) + 1},
    }
    for name, path in [
        ("list_document_history", "/api/document-history/global_context/global"),
        ("list_task_manuals", "/api/task-manuals"),
        ("list_roles", "/api/roles"),
    ]:
        lens, last = [], ""
        for _ in range(READS):
            st, last = req("GET", path, None, tok)
            assert st == 200, (name, st, last[:300])
            lens.append(len(last))
        out[name] = {"chars": lens[-1], "chars_min": min(lens), "chars_max": max(lens),
                     "reads": READS, "status": 200, "sample": last[:300]}

    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
