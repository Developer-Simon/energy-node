#!/usr/bin/env python3
"""Print the requirements the wheels in a directory need but do not provide.

Usage: missing_requirements.py <wheel-dir> <python-minor> <uname-machine>

Markers are evaluated for the *target* (given Python minor, Linux, given
machine), not for the interpreter running this script. `pip download
--python-version` evaluates markers such as python_version < "3.13" with the
build machine's own Python, so a build on 3.14 silently leaves out
typing_extensions that the node's 3.11 needs. One requirement per line, in
the form pip accepts.
"""
import email
import glob
import os
import sys
import zipfile

try:
    from packaging.markers import default_environment
    from packaging.requirements import Requirement
    from packaging.utils import canonicalize_name
    from packaging.version import Version
except ImportError:  # pip vendors it
    from pip._vendor.packaging.markers import default_environment
    from pip._vendor.packaging.requirements import Requirement
    from pip._vendor.packaging.utils import canonicalize_name
    from pip._vendor.packaging.version import Version


def target_environment(minor, machine):
    env = dict(default_environment())
    env.update({
        "python_version": minor,
        "python_full_version": minor + ".0",
        "implementation_name": "cpython",
        "platform_python_implementation": "CPython",
        "sys_platform": "linux",
        "platform_system": "Linux",
        "os_name": "posix",
        "platform_machine": machine,
        "extra": "",
    })
    return env


def read_metadata(path):
    with zipfile.ZipFile(path) as wheel:
        name = next(n for n in wheel.namelist() if n.endswith(".dist-info/METADATA"))
        return email.message_from_bytes(wheel.read(name))


def main(argv):
    wheel_dir, minor, machine = argv[1:4]
    env = target_environment(minor, machine)
    provided = {}
    requires = []
    for path in sorted(glob.glob(os.path.join(wheel_dir, "*.whl"))):
        meta = read_metadata(path)
        provided[canonicalize_name(meta["Name"])] = Version(meta["Version"])
        requires.extend(meta.get_all("Requires-Dist") or [])

    missing = {}
    for text in requires:
        req = Requirement(text)
        if req.marker is not None and not req.marker.evaluate(env):
            continue
        have = provided.get(canonicalize_name(req.name))
        if have is not None and req.specifier.contains(have, prereleases=True):
            continue
        missing[str(canonicalize_name(req.name)) + str(req.specifier)] = None
    print("\n".join(missing))


if __name__ == "__main__":
    main(sys.argv)
