"""Unit tests for LivelogPlugin worker-id / worker-count resolution (no xdist)."""

import pytest

from greendots_plugin.plugin import LivelogLoggingHandler, LivelogPlugin


def test_get_worker_id_explicit_override():
    p = LivelogPlugin()
    p._worker_id = 5
    assert p.get_worker_id(object()) == 5


def test_get_worker_id_defaults_to_zero_without_xdist():
    p = LivelogPlugin()
    assert p.get_worker_id(object()) == 0


def test_worker_count_explicit_override():
    p = LivelogPlugin()
    p._worker_count = 9
    assert p.worker_count == 9


def test_worker_count_defaults_to_one_without_xdist():
    p = LivelogPlugin()
    assert p.worker_count == 1


def test_is_worker_false_without_log_path():
    p = LivelogPlugin()
    assert p._log_path is None
    assert p.is_worker(object()) is False


def test_should_create_plan_false_when_skip_requested():
    p = LivelogPlugin()
    p._log_path = "/tmp/whatever"
    p._skip_plan = True
    assert p.should_create_plan(object()) is False


def test_should_create_plan_false_without_log_path():
    p = LivelogPlugin()
    assert p.should_create_plan(object()) is False


def test_get_worker_id_honors_explicit_zero():
    # 0 is falsy but a valid worker id; resolution must use isinstance, not truthiness
    p = LivelogPlugin()
    p._worker_id = 0
    assert p.get_worker_id(object()) == 0


def test_worker_count_honors_explicit_zero():
    p = LivelogPlugin()
    p._worker_count = 0
    assert p.worker_count == 0


# --- the invariant guards must raise loudly (they replaced asserts, survive -O) ---


def test_validate_worker_raises_without_handler():
    p = LivelogPlugin()
    p._log_path = "/tmp/whatever"
    p._handler = None
    with pytest.raises(RuntimeError, match="log handler"):
        p._validate_worker()


def test_validate_worker_raises_without_status_file(tmp_path):
    p = LivelogPlugin()
    p._log_path = "/tmp/whatever"
    p._handler = LivelogLoggingHandler(str(tmp_path / "h.jsonl"))
    p._status_file = None
    with pytest.raises(RuntimeError, match="status file"):
        p._validate_worker()


def test_capture_output_raises_without_handler():
    p = LivelogPlugin()
    p._handler = None
    with pytest.raises(RuntimeError, match="log handler"), p._capture_output():
        pass
