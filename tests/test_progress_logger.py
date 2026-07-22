"""Unit tests for ProgressLogger (root, children, split, sleep, done, disabled)."""

import io
import json

from greendots_plugin.plugin import ProgressLogger, StatusFile


def make_root(test="test_x"):
    """A root ProgressLogger writing into an in-memory status file."""
    buf = io.StringIO()
    return ProgressLogger(StatusFile(buf), test), buf


def progress(buf):
    return [json.loads(line) for line in buf.getvalue().splitlines() if line.strip()]


def percentages(buf):
    return [o["percentage"] for o in progress(buf)]


# --- root logger ---


def test_root_logs_and_throttles_sub_percent_changes():
    logger, buf = make_root("t")
    logger(0.02)  # logged (>1% change from 0)
    logger(0.025)  # throttled (<1% change)
    logger(0.5)  # logged
    assert percentages(buf) == [0.02, 0.5]
    for o in progress(buf):
        assert o["type"] == "progress" and o["test"] == "t"


def test_root_does_not_log_initial_zero():
    logger, buf = make_root()
    logger(0.0)
    assert progress(buf) == []


def test_root_clamps_below_zero_and_above_one():
    logger, buf = make_root()
    logger(0.5)  # logged 0.5
    logger(-1.0)  # clamp -> 0.0 (logged, 0.5 delta)
    logger(2.0)  # clamp -> 1.0 (done, forced)
    assert percentages(buf) == [0.5, 0.0, 1.0]


def test_root_always_emits_terminal_100_percent():
    # regression: the throttle must not swallow the final 1.0
    logger, buf = make_root()
    logger(0.995)  # logged
    logger(1.0)  # only 0.5% more, but completion forces the emit
    assert percentages(buf)[-1] == 1.0


def test_reaching_one_marks_done_and_ignores_further_calls():
    logger, buf = make_root()
    logger(1.0)
    assert logger._done is True
    logger(0.5)  # ignored once done
    assert percentages(buf) == [1.0]


# --- done() ---


def test_done_emits_terminal_with_test_key():
    # regression: a progress line without "test" crashes the server
    logger, buf = make_root("mytest")
    logger(0.4)
    logger.done()
    last = progress(buf)[-1]
    assert last["type"] == "progress"
    assert last["percentage"] == 1.0
    assert last["test"] == "mytest"


def test_done_is_idempotent():
    logger, buf = make_root()
    logger.done()
    count = len(progress(buf))
    logger.done()
    assert len(progress(buf)) == count


def test_child_done_forwards_completion_to_parent():
    logger, buf = make_root()
    a, _b = logger.split(2)  # a: base 0, fraction 0.5
    a.done()  # completes a's whole slice -> parent(0 + 0.5)
    assert percentages(buf) == [0.5]
    assert a._done is True


# --- disabled logger (no status file) ---


def test_disabled_logger_is_a_noop():
    logger = ProgressLogger(None, "t")
    assert logger._done is True
    # none of these should raise or write anywhere
    logger(0.5)
    logger.done()
    logger.sleep(0.01)


def test_disabled_sleep_returns_immediately():
    logger = ProgressLogger(None, "t")
    logger.sleep(5)  # would block for 5s if not short-circuited


# --- split() / children ---


def test_child_call_forwards_scaled_value_to_parent():
    logger, buf = make_root()
    a, _b = logger.split(2)  # a: base 0, fraction 0.5
    a(0.5)  # -> parent(0 + 0.5 * 0.5) = parent(0.25)
    assert percentages(buf) == [0.25]


def test_split_children_compose_to_full_progress():
    logger, buf = make_root()
    a, b = logger.split(2)
    a(1.0)  # -> parent 0.5
    b(1.0)  # -> parent 1.0 (done)
    assert percentages(buf) == [0.5, 1.0]
    assert logger._done is True


def test_nested_split_composes_correctly():
    # regression: split() must slice the child's own 0..1 range
    logger, buf = make_root()
    a, b = logger.split(2)
    a0, a1 = a.split(2)
    a0(1.0)  # a0 -> a(0.5) -> root(0.25)
    a1(1.0)  # a1 -> a(1.0) -> root(0.5)
    b(1.0)  # -> root(1.0)
    assert percentages(buf) == [0.25, 0.5, 1.0]


# --- sleep() ---


def test_root_sleep_fills_to_completion():
    logger, buf = make_root()
    logger.sleep(0.02)
    assert percentages(buf)[-1] == 1.0
    assert logger._done is True


def test_child_sleep_terminates_and_forwards():
    # regression: child sleep used to hang / raise (never sets its own _done)
    logger, buf = make_root()
    a, _b = logger.split(2)
    a.sleep(0.02)
    vals = percentages(buf)
    assert vals, "child sleep should forward progress to the parent"
    assert 0.0 < vals[-1] <= 0.5
    assert a._done is False  # a child never flips its own done flag


def test_throttle_exact_one_percent_is_not_logged():
    # boundary: the throttle uses strict > 1%, so exactly 1% from 0 is throttled
    logger, buf = make_root()
    logger(0.01)
    assert progress(buf) == []


def test_split_one_is_passthrough():
    logger, buf = make_root()
    (only,) = logger.split(1)
    only(0.5)  # -> parent(0.5)
    only.done()  # -> parent(1.0)
    assert percentages(buf) == [0.5, 1.0]
    assert logger._done is True


def test_split_three_composes_to_completion():
    # non-power-of-two splits accumulate float error; the near-1.0 composite is
    # snapped to exactly 1.0 so the bar still completes
    logger, buf = make_root()
    a, b, c = logger.split(3)
    a(1.0)
    b(1.0)
    c(1.0)
    assert percentages(buf)[-1] == 1.0
    assert logger._done is True


def test_done_after_reaching_one_is_noop():
    logger, buf = make_root()
    logger(1.0)
    count = len(progress(buf))
    logger.done()
    assert len(progress(buf)) == count  # no duplicate terminal line


def test_zero_duration_sleep_completes_immediately():
    logger, buf = make_root()
    logger.sleep(0)  # must not ZeroDivisionError
    assert percentages(buf)[-1] == 1.0
    assert logger._done is True
