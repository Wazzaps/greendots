"""Tests for the pytest-xdist integration paths.

pytest-xdist is a hard dev dependency, so these always run: worker-id resolution
is exercised via monkeypatching, and one end-to-end test drives a real ``-n 2``
distributed run. If xdist were ever missing these fail loudly rather than skip.
"""

import json

import pytest

from greendots_plugin.plugin import LivelogPlugin


def read_jsonl(path):
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def test_get_worker_id_parses_xdist_gw_number(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    monkeypatch.setattr(xdist, "is_xdist_worker", lambda s: True)
    monkeypatch.setattr(xdist, "get_xdist_worker_id", lambda s: "gw7")
    assert p.get_worker_id(object()) == 7


def test_get_worker_id_rejects_non_gw_id(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    monkeypatch.setattr(xdist, "is_xdist_worker", lambda s: True)
    monkeypatch.setattr(xdist, "get_xdist_worker_id", lambda s: "bad7")
    with pytest.raises(ValueError, match="Invalid worker id"):
        p.get_worker_id(object())


def test_is_worker_false_for_controller(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    p._log_path = "/tmp/whatever"
    monkeypatch.setattr(xdist, "is_xdist_controller", lambda s: True)
    monkeypatch.setattr(xdist, "is_xdist_master", lambda s: False)
    assert p.is_worker(object()) is False


def test_is_worker_true_for_actual_worker(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    p._log_path = "/tmp/whatever"
    monkeypatch.setattr(xdist, "is_xdist_controller", lambda s: False)
    monkeypatch.setattr(xdist, "is_xdist_master", lambda s: False)
    assert p.is_worker(object()) is True


def test_should_create_plan_only_on_gw0(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    p._log_path = "/tmp/whatever"
    monkeypatch.setattr(xdist, "is_xdist_controller", lambda s: False)
    monkeypatch.setattr(xdist, "is_xdist_master", lambda s: False)
    monkeypatch.setattr(xdist, "is_xdist_worker", lambda s: True)

    monkeypatch.setattr(xdist, "get_xdist_worker_id", lambda s: "gw0")
    assert p.should_create_plan(object()) is True

    monkeypatch.setattr(xdist, "get_xdist_worker_id", lambda s: "gw1")
    assert p.should_create_plan(object()) is False


def test_should_create_plan_false_for_controller(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    p._log_path = "/tmp/whatever"
    monkeypatch.setattr(xdist, "is_xdist_controller", lambda s: True)
    monkeypatch.setattr(xdist, "is_xdist_master", lambda s: False)
    monkeypatch.setattr(xdist, "is_xdist_worker", lambda s: False)
    assert p.should_create_plan(object()) is False


def test_is_worker_false_for_master(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    p._log_path = "/tmp/whatever"
    monkeypatch.setattr(xdist, "is_xdist_controller", lambda s: False)
    monkeypatch.setattr(xdist, "is_xdist_master", lambda s: True)
    assert p.is_worker(object()) is False


def test_should_create_plan_false_for_master(monkeypatch):
    import xdist

    p = LivelogPlugin()
    p._xdist_supported = True
    p._log_path = "/tmp/whatever"
    monkeypatch.setattr(xdist, "is_xdist_controller", lambda s: False)
    monkeypatch.setattr(xdist, "is_xdist_master", lambda s: True)
    monkeypatch.setattr(xdist, "is_xdist_worker", lambda s: False)
    assert p.should_create_plan(object()) is False


def test_worker_count_reads_xdist_env(monkeypatch):
    p = LivelogPlugin()
    p._xdist_supported = True
    monkeypatch.setenv("PYTEST_XDIST_WORKER_COUNT", "4")
    assert p.worker_count == 4


def test_distributed_run_records_all_tests_across_workers(pytester):
    pytester.makepyfile(
        """
        def test_a(): pass
        def test_b(): pass
        def test_c(): pass
        def test_d(): pass
        """
    )
    livelog = pytester.path / "livelog"
    # a real distributed run needs subprocess workers
    result = pytester.runpytest_subprocess("-n", "2", "--livelog", str(livelog))
    result.assert_outcomes(passed=4)

    # exactly the two worker files, and the plan exists once with worker_count 2
    assert (livelog / "status.0.jsonl").exists()
    assert (livelog / "status.1.jsonl").exists()
    plan = json.loads((livelog / "plan.json").read_text())
    assert plan["worker_count"] == 2

    # every test must be recorded with a passing finish somewhere across the
    # worker files — not merely that the files exist
    statuses = read_jsonl(livelog / "status.0.jsonl") + read_jsonl(livelog / "status.1.jsonl")
    finishes = {s["test"].split("::")[1]: s["outcome"] for s in statuses if s["type"] == "finish"}
    assert finishes == {
        "test_a": "passed",
        "test_b": "passed",
        "test_c": "passed",
        "test_d": "passed",
    }
