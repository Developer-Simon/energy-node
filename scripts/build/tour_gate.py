#!/usr/bin/env python3
"""Abschlussschritt der CI: wartet auf die Freigabe einer fehlenden Tour.

Hat ein Web-UI-Highlight des PRs weder Tour noch Ausnahme, bleibt der Build
gruen, und dieser Schritt fragt im PR nach: ein Kommentar nennt die Highlights,
ein Daumen hoch des Repo-Besitzers auf dem Kommentar gilt als erteilte Ausnahme.
Ohne Reaktion scheitert der Schritt nach dem Zeitlimit.

Die Freigabe liegt nur am Kommentar und nicht im Repo. Ein neuer Lauf desselben
PRs findet den Kommentar ueber MARKER wieder und fragt nicht noch einmal. Das
reicht auch fuers Dashboard: eine Ausnahme und eine fehlende Tour zeigen beide
keine Schritte (dashboard/internal/httpapi/onboarding.go).

    tour_gate.py --repo <checkout> --pr <N> --github-repo <owner/name>
                 --owner <login> --sha <sha> --run-url <url>
"""
import argparse
import json
import subprocess
import sys
import time
from pathlib import Path

import highlight_i18n as hi
import highlight_tours as ht

MARKER = "<!-- tour-gate -->"
# Nur eigene Kommentare zaehlen, sonst koennte jeder den Marker setzen.
BOT = "github-actions[bot]"
TIMEOUT_SECONDS = 600
POLL_SECONDS = 15
PAGE_SIZE = 100


class GhError(RuntimeError):
    pass


def gh_api(method, path, fields=None):
    """Ein Aufruf von gh api. Gibt das JSON der Antwort zurueck."""
    command = ["gh", "api", "--method", method, path]
    if fields is not None:
        command += ["--input", "-"]
    result = subprocess.run(command, input=json.dumps(fields) if fields is not None else None,
                            capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise GhError(f"gh api {method} {path}: {result.stderr.strip()}")
    return json.loads(result.stdout) if result.stdout.strip() else None


def _gap_lines(gaps):
    return "\n".join(f"- {component} {version}: {text}" for component, version, text in gaps)


def request_body(gaps, owner):
    return f"""{MARKER}
@{owner} this PR has a web UI highlight without a tour:

{_gap_lines(gaps)}

React with 👍 on this comment to grant the exemption. CI waits up to {TIMEOUT_SECONDS // 60} minutes after the build.

To add a tour instead, put it into the "Tour" section of the PR description. CI then starts again."""


def timed_out_body(gaps, owner, run_url):
    return f"""{MARKER}
@{owner} this PR has a web UI highlight without a tour:

{_gap_lines(gaps)}

CI stopped waiting, nobody granted the exemption in time. To continue, react with 👍 on this comment and then re-run the failed job of [this run]({run_url}).

To add a tour instead, put it into the "Tour" section of the PR description. CI then starts again."""


def granted_body(gaps, owner):
    return f"""{MARKER}
Exemption granted by {owner} with 👍. No tour is required for these highlights:

{_gap_lines(gaps)}"""


RESOLVED_BODY = f"""{MARKER}
Every web UI highlight of this PR has a tour or an exemption now. Nothing to acknowledge."""


def find_comment(api, github_repo, pr):
    page = 1
    while True:
        comments = api("GET", f"repos/{github_repo}/issues/{pr}/comments?per_page={PAGE_SIZE}&page={page}")
        for comment in comments:
            if comment["user"]["login"] == BOT and MARKER in comment["body"]:
                return comment
        if len(comments) < PAGE_SIZE:
            return None
        page += 1


def is_granted(api, github_repo, comment_id, owner):
    reactions = api("GET", f"repos/{github_repo}/issues/comments/{comment_id}/reactions"
                           f"?content=%2B1&per_page={PAGE_SIZE}")
    return any(reaction["user"]["login"] == owner for reaction in reactions)


def _write_comment(api, github_repo, comment, body):
    if comment["body"] != body:
        api("PATCH", f"repos/{github_repo}/issues/comments/{comment['id']}", {"body": body})
        comment["body"] = body


def gate(api, gaps, github_repo, pr, owner, sha, run_url, clock=time.monotonic, sleep=time.sleep):
    """0, sobald nichts fehlt oder der Besitzer freigegeben hat, sonst 1."""
    comment = find_comment(api, github_repo, pr)
    if not gaps:
        print(f"PR #{pr}: every web UI highlight has a tour or an exemption")
        if comment:
            _write_comment(api, github_repo, comment, RESOLVED_BODY)
        return 0
    for component, version, text in gaps:
        print(f"PR #{pr}, {component} {version}: no tour for \"{text}\"")

    if comment and is_granted(api, github_repo, comment["id"], owner):
        print(f"PR #{pr}: exemption already granted by {owner}")
        _write_comment(api, github_repo, comment, granted_body(gaps, owner))
        return 0

    waiting = request_body(gaps, owner)
    if comment:
        _write_comment(api, github_repo, comment, waiting)
    else:
        comment = api("POST", f"repos/{github_repo}/issues/{pr}/comments", {"body": waiting})
    api("POST", f"repos/{github_repo}/statuses/{sha}", {
        "state": "pending", "context": "CI", "target_url": run_url,
        "description": "Build passed, waiting for the tour exemption",
    })

    print(f"PR #{pr}: waiting up to {TIMEOUT_SECONDS}s for a reaction by {owner} on {comment.get('html_url', comment['id'])}")
    deadline = clock() + TIMEOUT_SECONDS
    while clock() < deadline:
        sleep(min(POLL_SECONDS, max(deadline - clock(), 0)))
        try:
            granted = is_granted(api, github_repo, comment["id"], owner)
        except GhError as error:
            # Ein Aussetzer der API darf das Warten nicht beenden.
            print(f"::warning::{error}")
            continue
        if granted:
            print(f"PR #{pr}: exemption granted by {owner}")
            _write_comment(api, github_repo, comment, granted_body(gaps, owner))
            return 0

    _write_comment(api, github_repo, comment, timed_out_body(gaps, owner, run_url))
    print(f"::error::PR #{pr}: no tour and no exemption granted in time")
    return 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--repo", required=True, help="checkout of the PR head")
    parser.add_argument("--pr", required=True, type=int)
    parser.add_argument("--github-repo", required=True)
    parser.add_argument("--owner", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--run-url", required=True)
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    gaps = []
    if args.pr >= ht.TOUR_REQUIRED_FROM_PR:
        translations = hi.load_translations(repo / hi.TRANSLATIONS_DIR)
        table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
        gaps = ht.missing(repo, table, args.pr, translations)
    try:
        return gate(gh_api, gaps, args.github_repo, args.pr, args.owner, args.sha, args.run_url)
    except GhError as error:
        print(f"::error::{error}")
        return 1


if __name__ == "__main__":
    sys.exit(main())
