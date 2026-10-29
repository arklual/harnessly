#!/usr/bin/env python3
"""Rebrand a web/dist taken out of a release binary (extract_embed.py) as klodik: the name, and Claude's spark for the
"h." mark, the same as web/public and web/app carry in this fork.

    python3 -I scripts/klodik-dist.py web/dist

Hashed assets are served as immutable, so an asset whose bytes change gets a new name, and so does every asset that
names it, up to index.html.
"""
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
NAME = "klodik.tech"
# The spark's path (24×24) and how it sits on the 1024 tile; the colour is Claude's terracotta.
LOGO_SVG = open(os.path.join(ROOT, "web/public/logo.svg")).read()
SPARK = re.search(r'd="([^"]+)"', LOGO_SVG.split("<path", 1)[1]).group(1)
TRANSFORM, FILL = "translate(192 192) scale(26.6667)", "#D97757"

# The compiled "h." mark: the path of the letter, then the lime dot.
MARK = re.compile(
    r"transform:`translate\(235\.1 712\.2\)`,d:`M29\.12 -400\.4[^`]*`,fill:`#F2F2EE`\}\)"
    r",\(0,(\w+)\.jsx\)\(`circle`,\{cx:`715\.6`,cy:`668`,r:`44\.2`,fill:`#D4FF3A`\}\)"
)
FIXED_NAMES = {"index.html", "guest.html", "sw.js", "manifest.json", "logo.svg", "preview-bridge.js"}


def rebrand_js(s):
    # The dot goes as an empty group, so the children array keeps its shape.
    return MARK.sub(lambda m: f"transform:`{TRANSFORM}`,d:`{SPARK}`,fill:`{FILL}`}}),null", s)


def main(dist):
    with open(os.path.join(dist, "logo.svg"), "w") as f:
        f.write(LOGO_SVG)
    mp = os.path.join(dist, "manifest.json")
    m = json.load(open(mp))
    m["name"] = m["short_name"] = NAME
    with open(mp, "w") as f:
        json.dump(m, f, indent=2, ensure_ascii=False)
        f.write("\n")
    for page in ("index.html",):
        p = os.path.join(dist, page)
        s = open(p).read()
        s = s.replace('content="harnessly"', f'content="{NAME}"').replace("<title>harnessly</title>", f"<title>{NAME}</title>")
        open(p, "w").write(s)

    texts = {}
    for d, _, files in os.walk(dist):
        for n in files:
            if n.endswith((".js", ".css", ".html")):
                p = os.path.join(d, n)
                texts[p] = open(p, encoding="utf-8").read()
    changed = set()
    for p, s in texts.items():
        if p.endswith(".js"):
            t = rebrand_js(s)
            if t != s:
                texts[p] = t
                changed.add(p)
    # Rename changed hashed assets (name-HASH.js -> name-HASHk.js) and rewrite who names them, until nothing moves.
    renamed = {}
    queue = [p for p in changed]
    while queue:
        p = queue.pop()
        base = os.path.basename(p)
        if base in FIXED_NAMES or p in renamed:
            continue
        stem, ext = os.path.splitext(base)
        new = os.path.join(os.path.dirname(p), stem + "k" + ext)
        renamed[p] = new
        old_base, new_base = base, os.path.basename(new)
        for q, s in texts.items():
            if old_base in s:
                texts[q] = s.replace(old_base, new_base)
                if q not in changed:
                    changed.add(q)
                    queue.append(q)
    for p in changed:
        with open(p, "w", encoding="utf-8") as f:
            f.write(texts[p])
    for old, new in renamed.items():
        os.rename(old, new)
    marks = sum(1 for p in changed if SPARK in texts[p])
    print(f"{NAME}: {marks} marks replaced, {len(changed)} files rewritten, {len(renamed)} assets renamed")
    if marks == 0:
        sys.exit("no compiled mark found: the release changed it, update MARK")


if __name__ == "__main__":
    main(sys.argv[1])
