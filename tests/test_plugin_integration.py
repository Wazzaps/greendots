"""End-to-end tests driving a real pytest session via the ``pytester`` fixture
and asserting on the plan/status/log files the plugin writes."""

import json


def read_jsonl(path):
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def run(pytester, *args):
    """Run an inner pytest with --livelog and return (result, livelog_dir)."""
    livelog = pytester.path / "livelog"
    result = pytester.runpytest("--livelog", str(livelog), *args)
    return result, livelog


def log_file_for(livelog, name):
    plan = json.loads((livelog / "plan.json").read_text())
    return next(it["log_file"] for g in plan["groups"].values() for it in g if it["name"] == name)


# --- opt-in behaviour ---


def test_without_livelog_nothing_is_written(pytester):
    pytester.makepyfile("def test_x(): pass")
    result = pytester.runpytest()
    result.assert_outcomes(passed=1)
    assert not (pytester.path / "livelog").exists()
    assert not (pytester.path / "plan.json").exists()


# --- plan.json ---


def test_plan_groups_and_items(pytester):
    pytester.makepyfile(
        test_mod="""
        import pytest

        @pytest.mark.parametrize("arch", ["x86", "arm"])
        @pytest.mark.parametrize("n", [1, 2])
        def test_a(arch, n):
            pass

        def test_b():
            pass
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(passed=5)
    plan = json.loads((livelog / "plan.json").read_text())
    assert plan["worker_count"] == 1
    assert set(plan["groups"]) == {"test_mod"}
    # test_b has no params, so the intersection of param keys is empty
    assert plan["row_params"] == []
    items = plan["groups"]["test_mod"]
    assert {it["name"] for it in items} == {"test_a", "test_b"}
    a = next(it for it in items if it["params"] == {"arch": "x86", "n": 1})
    assert a["id"] == "test_mod.py::test_a[1-x86]"
    assert a["log_file"].endswith(".log.jsonl")


def test_row_params_is_intersection_of_param_keys(pytester):
    pytester.makepyfile(
        """
        import pytest

        @pytest.mark.parametrize("arch", ["x86", "arm"])
        @pytest.mark.parametrize("size", [1, 2])
        def test_a(arch, size):
            pass

        @pytest.mark.parametrize("arch", ["x86", "arm"])
        def test_b(arch):
            pass
        """
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    assert plan["row_params"] == ["arch"]  # only key shared by every test


def test_skip_plan_creation_keeps_existing_plan(pytester):
    livelog = pytester.path / "livelog"
    livelog.mkdir()
    (livelog / "plan.json").write_text('{"sentinel": true}')
    pytester.makepyfile("def test_x(): pass")
    result = pytester.runpytest("--livelog", str(livelog), "--skip-plan-creation")
    result.assert_outcomes(passed=1)
    assert json.loads((livelog / "plan.json").read_text()) == {"sentinel": True}
    assert (livelog / "status.0.jsonl").exists()  # status still streamed


def test_worker_count_override_in_plan(pytester):
    pytester.makepyfile("def test_x(): pass")
    livelog = pytester.path / "livelog"
    result = pytester.runpytest("--livelog", str(livelog), "--livelog-worker-count", "5")
    result.assert_outcomes(passed=1)
    assert json.loads((livelog / "plan.json").read_text())["worker_count"] == 5


def test_worker_id_override_names_status_file(pytester):
    pytester.makepyfile("def test_x(): pass")
    livelog = pytester.path / "livelog"
    result = pytester.runpytest("--livelog", str(livelog), "--livelog-worker-id", "3")
    result.assert_outcomes(passed=1)
    assert (livelog / "status.3.jsonl").exists()
    assert not (livelog / "status.0.jsonl").exists()


# --- status stream ---


def test_status_stream_start_phases_and_outcomes(pytester):
    pytester.makepyfile(
        """
        import pytest

        def test_pass():
            pass

        def test_fail():
            assert 1 == 2

        @pytest.mark.skip(reason="later")
        def test_skip():
            pass
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(passed=1, failed=1, skipped=1)
    statuses = read_jsonl(livelog / "status.0.jsonl")

    starts = {s["test"].split("::")[1] for s in statuses if s["type"] == "start"}
    assert {"test_pass", "test_fail", "test_skip"} <= starts

    assert {"setup", "call", "teardown"} <= {s["type"] for s in statuses}

    finishes = {s["test"].split("::")[1]: s["outcome"] for s in statuses if s["type"] == "finish"}
    assert finishes["test_pass"] == "passed"
    assert finishes["test_fail"] == "failed"
    assert finishes["test_skip"] == "skipped"


def test_failure_records_exception_text(pytester):
    pytester.makepyfile(
        """
        def test_fail():
            value = 41
            assert value == 42
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(failed=1)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    failed = [s for s in statuses if s.get("outcome") == "failed" and "exception" in s]
    assert failed, "a failed report must carry the exception text"
    assert "assert" in failed[0]["exception"]


def test_skip_records_reason(pytester):
    pytester.makepyfile(
        """
        import pytest

        @pytest.mark.skip(reason="because reasons")
        def test_skip():
            pass
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(skipped=1)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    reasons = [s["reason"] for s in statuses if "reason" in s]
    assert reasons and "because reasons" in reasons[0]


# --- per-test log files ---


def test_per_test_log_captures_logging(pytester):
    pytester.makepyfile(
        """
        import logging

        def test_log():
            logging.getLogger("app.sub").warning("captured %s", "message")
        """
    )
    _result, livelog = run(pytester)
    entries = read_jsonl(livelog / log_file_for(livelog, "test_log"))
    assert any(
        e.get("name") == "app.sub"
        and e.get("level") == "WARNING"
        and "captured message" in e.get("message", "")
        for e in entries
    )


def test_per_test_log_captures_stdout_and_stderr(pytester):
    pytester.makepyfile(
        """
        import sys

        def test_io():
            print("a stdout line")
            print("a stderr line", file=sys.stderr)
        """
    )
    _result, livelog = run(pytester)
    entries = read_jsonl(livelog / log_file_for(livelog, "test_io"))
    assert any(
        e.get("name") == "stdout" and "a stdout line" in e.get("message", "") for e in entries
    )
    assert any(
        e.get("name") == "stderr" and "a stderr line" in e.get("message", "") for e in entries
    )


def test_log_file_exists_for_every_planned_test(pytester):
    pytester.makepyfile(
        """
        import pytest

        @pytest.mark.parametrize("v", [1, 2, 3])
        def test_x(v):
            pass
        """
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    for group in plan["groups"].values():
        for it in group:
            assert (livelog / it["log_file"]).exists(), it["log_file"]


# --- live_progress fixture ---


def test_live_progress_fixture_streams_progress(pytester):
    pytester.makepyfile(
        """
        def test_prog(live_progress):
            live_progress(0.5)
            live_progress.done()
        """
    )
    _result, livelog = run(pytester)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    progs = [s for s in statuses if s["type"] == "progress" and s["test"].endswith("test_prog")]
    assert any(p["percentage"] == 0.5 for p in progs)
    assert any(p["percentage"] == 1.0 for p in progs)
    assert all("test" in p for p in progs)  # every progress line carries a test id


def test_live_progress_without_livelog_is_harmless(pytester):
    # the fixture resolves to a disabled no-op logger when livelog is off
    pytester.makepyfile(
        """
        def test_prog(live_progress):
            live_progress(0.5)
            live_progress.done()
            live_progress.sleep(0.01)
        """
    )
    result = pytester.runpytest()
    result.assert_outcomes(passed=1)
    assert not (pytester.path / "livelog").exists()


# --- determinism / naming ---


def test_row_params_are_sorted_deterministically(pytester):
    pytester.makepyfile(
        """
        import pytest

        @pytest.mark.parametrize("size", [1, 2])
        @pytest.mark.parametrize("arch", ["x86", "arm"])
        def test_a(arch, size):
            pass
        """
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    # both keys are shared by every test; order must be sorted, not set-iteration order
    assert plan["row_params"] == ["arch", "size"]


def test_plan_log_file_derives_from_nodeid_and_is_unique(pytester):
    from greendots_plugin.plugin import _create_log_name

    pytester.makepyfile(
        """
        import pytest

        @pytest.mark.parametrize("v", [1, 2, 3])
        def test_x(v):
            pass
        """
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    items = [it for g in plan["groups"].values() for it in g]
    for it in items:
        assert it["log_file"] == _create_log_name(it["id"])
    assert len({it["log_file"] for it in items}) == len(items)  # no name collisions


def test_special_char_nodeid_log_file_is_created(pytester):
    # a param value with a filesystem-special char forces name mangling end-to-end
    pytester.makepyfile(
        """
        import pytest

        @pytest.mark.parametrize("path", ["a/b"])
        def test_x(path):
            print("hi")
        """
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    it = next(i for g in plan["groups"].values() for i in g)
    assert "/" not in it["log_file"]
    assert (livelog / it["log_file"]).exists()  # the mangled file was actually opened


def test_plan_groups_multiple_modules(pytester):
    pytester.makepyfile(
        test_mod_a="def test_a(): pass",
        test_mod_b="def test_b(): pass",
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    assert set(plan["groups"]) == {"test_mod_a", "test_mod_b"}


def test_non_function_items_are_skipped_not_fatal(pytester):
    # --doctest-modules yields a DoctestItem (a non-Function collection item);
    # it must be skipped from the plan with a warning, never crash plan creation
    pytester.makepyfile(
        test_mod='''
        def add(a, b):
            """
            >>> add(1, 2)
            3
            """
            return a + b

        def test_real():
            pass
        '''
    )
    result, livelog = run(pytester, "--doctest-modules")
    result.assert_outcomes(passed=2)  # doctest + real test both run, no crash
    plan = json.loads((livelog / "plan.json").read_text())
    names = {it["name"] for g in plan["groups"].values() for it in g}
    assert names == {"test_real"}  # only the Function item is planned
    result.stdout.fnmatch_lines(["*skipping non-Function*"])  # warned, not silent


def test_custom_param_serialized_via_livelog_format(pytester):
    # non-JSON-native params reach plan.json through json_encode_default
    pytester.makepyfile(
        conftest="""
        import pytest

        class Weird:
            def __livelog_format__(self):
                return "weird!"

        def pytest_generate_tests(metafunc):
            metafunc.parametrize("obj", [Weird()])
        """,
        test_mod="def test_x(obj): pass",
    )
    _result, livelog = run(pytester)
    plan = json.loads((livelog / "plan.json").read_text())
    it = next(i for g in plan["groups"].values() for i in g)
    assert it["params"]["obj"] == "weird!"


# --- worst-outcome precedence across phases ---


def test_finish_outcome_failed_when_teardown_fails(pytester):
    pytester.makepyfile(
        """
        import pytest

        @pytest.fixture
        def boom():
            yield
            raise RuntimeError("teardown boom")

        def test_ok(boom):
            pass  # the call passes, but teardown raises
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(passed=1, errors=1)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    finish = next(s for s in statuses if s["type"] == "finish" and s["test"].endswith("test_ok"))
    assert finish["outcome"] == "failed"  # 'failed' must win over the passing call


def test_setup_failure_recorded_and_finishes_failed(pytester):
    pytester.makepyfile(
        """
        import pytest

        @pytest.fixture
        def boom():
            raise RuntimeError("setup boom")

        def test_needs(boom):
            pass
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(errors=1)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    setup = [s for s in statuses if s.get("type") == "setup" and s["test"].endswith("test_needs")]
    assert setup and setup[0]["outcome"] == "failed"
    assert "exception" in setup[0]
    finish = next(
        s for s in statuses if s["type"] == "finish" and s["test"].endswith("test_needs")
    )
    assert finish["outcome"] == "failed"


def test_each_phase_records_its_outcome(pytester):
    pytester.makepyfile("def test_ok(): pass")
    _result, livelog = run(pytester)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    phases = {
        s["type"]: s.get("outcome") for s in statuses if s["type"] in ("setup", "call", "teardown")
    }
    assert phases == {"setup": "passed", "call": "passed", "teardown": "passed"}


def test_phase_order_for_a_single_test(pytester):
    pytester.makepyfile("def test_one(): pass")
    _result, livelog = run(pytester)
    statuses = read_jsonl(livelog / "status.0.jsonl")
    types = [s["type"] for s in statuses if s["test"].endswith("test_one")]
    assert types == ["start", "setup", "call", "teardown", "finish"]


# --- log capture edge cases ---


def test_failed_test_log_has_clean_traceback(pytester):
    pytester.makepyfile(
        """
        def test_fail():
            value = 41
            assert value == 42
        """
    )
    result, livelog = run(pytester)
    result.assert_outcomes(failed=1)
    entries = read_jsonl(livelog / log_file_for(livelog, "test_fail"))
    text = "\n".join(e.get("message", "") for e in entries)
    assert any(e.get("level") == "ERROR" for e in entries)  # failure mirrored into the log
    assert "41" in text and "42" in text  # the real assertion-rewritten traceback
    assert "NoneType: None" not in text  # regression: logging.exception used to add this


def test_capture_during_setup_and_teardown(pytester):
    pytester.makepyfile(
        """
        import sys
        import pytest

        @pytest.fixture
        def noisy():
            print("setup out")
            print("setup err", file=sys.stderr)
            yield
            print("teardown out")

        def test_uses(noisy):
            pass
        """
    )
    _result, livelog = run(pytester)
    entries = read_jsonl(livelog / log_file_for(livelog, "test_uses"))
    msgs = {(e.get("name"), e.get("message")) for e in entries}
    assert ("stdout", "setup out") in msgs
    assert ("stderr", "setup err") in msgs
    assert ("stdout", "teardown out") in msgs


def test_trailing_partial_line_is_captured(pytester):
    pytester.makepyfile(
        """
        import sys

        def test_partial():
            sys.stdout.write("no trailing newline")
        """
    )
    _result, livelog = run(pytester)
    entries = read_jsonl(livelog / log_file_for(livelog, "test_partial"))
    assert any(
        e.get("name") == "stdout" and e.get("message") == "no trailing newline" for e in entries
    )
