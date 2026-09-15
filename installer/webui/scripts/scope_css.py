#!/usr/bin/env python3
"""Druckt eine Vorlagen-CSS als eingeschachtelten Block fuer screens.css.

Aufruf: python3 webui/scripts/scope_css.py <Vorlage> <screen>
Jeder Selektor bekommt das Praefix .app[data-screen="<screen>"], auch
innerhalb von @media. @keyframes bleiben global. Nur Standardbibliothek.
"""
import pathlib
import re
import sys


def blocks(src):
    i = 0
    while i < len(src):
        open_ = src.find("{", i)
        if open_ < 0:
            return
        prelude = " ".join(src[i:open_].split())
        depth, j = 1, open_ + 1
        while j < len(src) and depth:
            depth += {"{": 1, "}": -1}.get(src[j], 0)
            j += 1
        yield prelude, src[open_ + 1:j - 1]
        i = j


def scoped(src, screen):
    out = []
    for prelude, body in blocks(src):
        if prelude.startswith("@media") or prelude.startswith("@supports"):
            out.append("%s{%s}" % (prelude, "".join(scoped(body, screen))))
        elif prelude.startswith("@keyframes"):
            out.append("%s{%s}" % (prelude, " ".join(body.split())))
        elif prelude:
            selector = ",".join('.app[data-screen="%s"] %s' % (screen, part.strip()) for part in prelude.split(","))
            out.append("%s{%s}" % (selector, " ".join(body.split())))
    return out


def main():
    name, screen = sys.argv[1], sys.argv[2]
    path = pathlib.Path(__file__).resolve().parent.parent / "test" / "reference" / (name + ".css")
    src = re.sub(r"/\*.*?\*/", "", path.read_text(encoding="utf-8"), flags=re.S)
    print("/* == Vorlage: %s == */" % name)
    print("\n".join(scoped(src, screen)))
    print("/* == Ende Vorlage == */")


if __name__ == "__main__":
    main()
