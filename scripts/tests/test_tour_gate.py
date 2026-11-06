import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import tour_gate as tg

GAPS = [("Dashboard", "v0.11.4", "Animate the page")]
OWNER = "the-owner"
REPO = "the-owner/the-repo"
RUN = "https://example.test/run/1"


class FakeGitHub:
    """Haelt Kommentare und Reaktionen. react_at legt die Reaktion erst an, wenn
    die Uhr diesen Stand erreicht hat."""

    def __init__(self, comments=(), reactions=(), react_at=None, fail_reads=0):
        self.comments = [dict(comment) for comment in comments]
        self.reactions = list(reactions)
        self.react_at = react_at
        self.fail_reads = fail_reads
        self.now = 0
        self.calls = []

    def clock(self):
        return self.now

    def sleep(self, seconds):
        self.now += seconds

    def api(self, method, path, fields=None):
        self.calls.append((method, path, fields))
        if method == "GET" and "/reactions" in path:
            if self.fail_reads:
                self.fail_reads -= 1
                raise tg.GhError("boom")
            if self.react_at is not None and self.now >= self.react_at:
                return [{"user": {"login": OWNER}}]
            return [{"user": {"login": login}} for login in self.reactions]
        if method == "GET":
            return self.comments if "page=1" in path else []
        if method == "POST" and path.endswith("/comments"):
            comment = {"id": 7, "body": fields["body"], "user": {"login": tg.BOT}}
            self.comments.append(comment)
            return comment
        if method == "PATCH":
            self.comments[0]["body"] = fields["body"]
        return {}

    def posted(self, ending):
        return [call for call in self.calls if call[0] == "POST" and call[1].endswith(ending)]


def run(github, gaps=GAPS):
    return tg.gate(github.api, gaps, REPO, 213, OWNER, "abc", RUN, clock=github.clock, sleep=github.sleep)


def own_comment(body=tg.MARKER):
    return {"id": 7, "body": body, "user": {"login": tg.BOT}}


def test_nothing_missing_passes_without_a_comment():
    github = FakeGitHub()
    assert run(github, gaps=[]) == 0
    assert github.posted("/comments") == []


def test_a_tour_added_later_closes_the_open_question():
    github = FakeGitHub(comments=[own_comment()])
    assert run(github, gaps=[]) == 0
    assert github.comments[0]["body"] == tg.RESOLVED_BODY


def test_missing_tour_posts_a_comment_naming_the_highlight_and_the_owner():
    github = FakeGitHub(react_at=30)
    assert run(github) == 0
    body = github.posted("/comments")[0][2]["body"]
    assert body.startswith(tg.MARKER)
    assert "Animate the page" in body
    assert f"@{OWNER}" in body


def test_waiting_marks_the_commit_status_pending():
    github = FakeGitHub(react_at=30)
    run(github)
    status = github.posted("/statuses/abc")[0][2]
    assert (status["state"], status["context"]) == ("pending", "CI")


def test_reaction_of_the_owner_passes_and_is_recorded_in_the_comment():
    github = FakeGitHub(react_at=30)
    assert run(github) == 0
    assert github.now == 30
    assert "Exemption granted by the-owner" in github.comments[0]["body"]


def test_reaction_of_someone_else_does_not_count():
    github = FakeGitHub(reactions=["somebody"])
    assert run(github) == 1


def test_no_reaction_fails_after_ten_minutes_and_explains_the_rerun():
    github = FakeGitHub()
    assert run(github) == 1
    assert github.now == 600
    assert RUN in github.comments[0]["body"]
    assert "re-run" in github.comments[0]["body"]


def test_reaction_at_the_last_poll_still_counts():
    github = FakeGitHub(react_at=600)
    assert run(github) == 0


def test_a_new_run_reuses_the_granted_comment_without_waiting():
    github = FakeGitHub(comments=[own_comment()], reactions=[OWNER])
    assert run(github) == 0
    assert github.now == 0
    assert github.posted("/comments") == []
    assert github.posted("/statuses/abc") == []


def test_a_new_run_after_a_timeout_asks_again_in_the_same_comment():
    github = FakeGitHub(comments=[own_comment(tg.MARKER + "\ntimed out")], react_at=15)
    # Der erste Lesezugriff vor dem Warten sieht noch keine Reaktion.
    assert run(github) == 0
    assert github.posted("/comments") == []
    asked = [call for call in github.calls if call[0] == "PATCH"][0][2]["body"]
    assert asked == tg.request_body(GAPS, OWNER)


def test_a_marker_in_a_foreign_comment_is_ignored():
    github = FakeGitHub(comments=[{"id": 9, "body": tg.MARKER, "user": {"login": "somebody"}}], reactions=[OWNER])
    assert tg.find_comment(github.api, REPO, 213) is None


def test_a_failing_read_while_waiting_does_not_end_the_wait():
    github = FakeGitHub(react_at=45, fail_reads=2)
    assert run(github) == 0
    assert github.now == 45
