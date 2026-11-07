#!/usr/bin/env python3
"""Pin the third-party wheels of a download to the index they came from.

Usage: wheel_pins.py <wheel-dir> <index-url>...

Prints a JSON list [{"file", "url", "sha256"}] sorted by file. For each
wheel the PEP 503 simple page <index>/<normalised-name>/ is searched in the
given order (piwheels before PyPI). The first link with exactly this file
name whose #sha256= fragment equals the SHA-256 of the downloaded file wins:
its absolute address goes into "url" without the fragment. Indexes can hold
the same file name with different bytes (piwheels and PyPI do), so a name
match alone is not enough. A wheel with no link of matching name and hash in
any index exits with status 1 and names the file.
"""
import hashlib
import json
import os
import re
import sys
import urllib.parse
import urllib.request
from html.parser import HTMLParser


class _Links(HTMLParser):
    def __init__(self):
        super().__init__()
        self.hrefs = []

    def handle_starttag(self, tag, attrs):
        if tag == "a":
            for key, value in attrs:
                if key == "href" and value:
                    self.hrefs.append(value)


def normalise(name):
    return re.sub(r"[-_.]+", "-", name).lower()


def distribution_name(filename):
    return normalise(filename.split("-", 1)[0])


def sha256_of(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def find_links(index, filename):
    """All (absolute url without fragment, sha256 or None) links for the file."""
    page_url = index.rstrip("/") + "/" + distribution_name(filename) + "/"
    try:
        with urllib.request.urlopen(page_url, timeout=60) as response:
            page = response.read().decode("utf-8", "replace")
    except OSError:
        return []
    parser = _Links()
    parser.feed(page)
    links = []
    for href in parser.hrefs:
        absolute = urllib.parse.urljoin(page_url, href)
        parts = urllib.parse.urlsplit(absolute)
        if os.path.basename(parts.path) != filename:
            continue
        sha = None
        if parts.fragment.startswith("sha256="):
            sha = parts.fragment[len("sha256="):]
        url = urllib.parse.urlunsplit(parts._replace(fragment=""))
        links.append((url, sha))
    return links


def pin(wheel_dir, indexes):
    pins = []
    for filename in sorted(os.listdir(wheel_dir)):
        if not filename.endswith(".whl"):
            continue
        actual = sha256_of(os.path.join(wheel_dir, filename))
        match = None
        for index in indexes:
            for url, sha in find_links(index, filename):
                if sha == actual:
                    match = url
                    break
            if match is not None:
                break
        if match is None:
            sys.exit("%s: kein Index hat diese Datei mit dem SHA-256 der Datei" % filename)
        pins.append({"file": filename, "url": match, "sha256": actual})
    return sorted(pins, key=lambda item: item["file"])


def main(argv):
    if len(argv) < 3:
        sys.exit(__doc__.strip())
    print(json.dumps(pin(argv[1], argv[2:]), indent=2))


if __name__ == "__main__":
    main(sys.argv)
