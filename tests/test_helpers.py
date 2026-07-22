"""Unit tests for the plugin's pure helpers and small IO classes."""

import io
import json
import logging

from greendots_plugin.plugin import (
    LivelogLoggingHandler,
    LivelogStdoutHandler,
    StatusFile,
    _create_log_name,
    _hash,
    json_encode_default,
)

# --- _hash ---


def test_hash_is_deterministic():
    assert _hash("abc") == _hash("abc")


def test_hash_differs_for_different_input():
    assert _hash("abc") != _hash("abd")


def test_hash_returns_nonempty_str():
    h = _hash("some::node::id")
    assert isinstance(h, str) and h


# --- _create_log_name ---


def test_create_log_name_simple():
    assert _create_log_name("test_mod.py::test_x") == "test_mod.py::test_x.log.jsonl"


def test_create_log_name_brackets_are_not_special():
    # parametrization brackets are legal filename chars, kept verbatim
    assert _create_log_name("test_mod.py::test_x[a-1]") == "test_mod.py::test_x[a-1].log.jsonl"


def test_create_log_name_special_char_replaced_and_hashed():
    name = _create_log_name("sub/test_mod.py::test_x")  # contains '/'
    assert name.endswith(".log.jsonl")
    assert "/" not in name
    assert _create_log_name("sub/test_mod.py::test_x") == name  # deterministic


def test_create_log_name_special_chars_stay_unique():
    # both mangle to "a_b" but the appended hash keeps them distinct
    assert _create_log_name("a/b") != _create_log_name("a\\b")


def test_create_log_name_long_is_truncated_but_unique():
    n1 = _create_log_name("x" * 200)
    n2 = _create_log_name("y" + "x" * 199)
    assert len(n1) < 200  # truncated
    assert n1 != n2  # hash keeps distinct
    assert n1.endswith(".log.jsonl")


def test_create_log_name_never_leaks_special_chars():
    name = _create_log_name('weird<>:"/\\|?*name')
    for bad in '<>"/\\|?*':
        assert bad not in name[: -len(".log.jsonl")]


# --- json_encode_default ---


def test_json_encode_default_repr_fallback():
    obj = object()
    assert json_encode_default(obj) == repr(obj)


def test_json_encode_default_uses_livelog_format():
    class Custom:
        def __livelog_format__(self):
            return "custom-repr"

    assert json_encode_default(Custom()) == "custom-repr"


# --- StatusFile ---


def test_statusfile_adds_time_when_missing():
    buf = io.StringIO()
    StatusFile(buf).log({"type": "x"})
    obj = json.loads(buf.getvalue())
    assert isinstance(obj["time"], float)


def test_statusfile_preserves_explicit_time():
    buf = io.StringIO()
    StatusFile(buf).log({"type": "x", "time": 123.5})
    assert json.loads(buf.getvalue())["time"] == 123.5


def test_statusfile_writes_one_json_object_per_line():
    buf = io.StringIO()
    sf = StatusFile(buf)
    sf.log({"a": 1})
    sf.log({"b": 2})
    lines = [line for line in buf.getvalue().splitlines() if line.strip()]
    assert len(lines) == 2
    assert json.loads(lines[0])["a"] == 1
    assert json.loads(lines[1])["b"] == 2


# --- LivelogLoggingHandler ---


def test_logging_handler_writes_structured_record(tmp_path):
    path = tmp_path / "handler.jsonl"
    handler = LivelogLoggingHandler(str(path))
    record = logging.LogRecord(
        "my.logger", logging.WARNING, __file__, 1, "hi %s", ("there",), None
    )
    handler.emit(record)
    obj = json.loads(path.read_text().splitlines()[0])
    assert obj["name"] == "my.logger"
    assert obj["level"] == "WARNING"
    assert obj["message"] == "hi there"
    assert isinstance(obj["time"], float)


# --- LivelogStdoutHandler ---


def _stdout_entries(tmp_path):
    return [json.loads(line) for line in (tmp_path / "log.jsonl").read_text().splitlines() if line]


def test_stdout_handler_buffers_partial_lines(tmp_path):
    parent = io.StringIO()
    handler = LivelogLoggingHandler(str(tmp_path / "log.jsonl"))
    stream = LivelogStdoutHandler(True, parent, handler)  # error=True -> stderr
    stream.write("partial ")  # no newline -> buffered, not logged yet
    stream.write("line\n")  # completes the line -> logged as one message
    entries = _stdout_entries(tmp_path)
    assert any(
        e["name"] == "stderr" and e["level"] == "ERROR" and e["message"] == "partial line"
        for e in entries
    )
    # the original stream still received everything verbatim
    assert parent.getvalue() == "partial line\n"


def test_stdout_handler_writelines_delegates_to_write(tmp_path):
    parent = io.StringIO()
    handler = LivelogLoggingHandler(str(tmp_path / "log.jsonl"))
    stream = LivelogStdoutHandler(False, parent, handler)  # error=False -> stdout
    stream.writelines(["hello ", "world\n"])
    assert parent.getvalue() == "hello world\n"
    entries = _stdout_entries(tmp_path)
    assert any(e["name"] == "stdout" and e["message"] == "hello world" for e in entries)


def test_stdout_handler_accumulates_consecutive_partial_writes(tmp_path):
    # regression: consecutive partial writes must APPEND, not overwrite _last_line
    parent = io.StringIO()
    handler = LivelogLoggingHandler(str(tmp_path / "log.jsonl"))
    stream = LivelogStdoutHandler(False, parent, handler)
    stream.write("a")
    stream.write("b")
    stream.write("c\n")
    assert [e["message"] for e in _stdout_entries(tmp_path)] == ["abc"]
    assert parent.getvalue() == "abc\n"


def test_stdout_handler_splits_a_multiline_single_write(tmp_path):
    parent = io.StringIO()
    handler = LivelogLoggingHandler(str(tmp_path / "log.jsonl"))
    stream = LivelogStdoutHandler(False, parent, handler)
    stream.write("l1\nl2\nl3\n")
    assert [e["message"] for e in _stdout_entries(tmp_path)] == ["l1", "l2", "l3"]
    assert parent.getvalue() == "l1\nl2\nl3\n"


def test_stdout_handler_flush_partial_emits_trailing_text(tmp_path):
    # regression: a trailing partial line (no newline) must not be lost
    parent = io.StringIO()
    handler = LivelogLoggingHandler(str(tmp_path / "log.jsonl"))
    stream = LivelogStdoutHandler(False, parent, handler)
    stream.write("no trailing newline")
    assert _stdout_entries(tmp_path) == []  # buffered, not emitted yet
    stream.flush_partial()
    assert [e["message"] for e in _stdout_entries(tmp_path)] == ["no trailing newline"]


def test_stdout_handler_strips_crlf(tmp_path):
    parent = io.StringIO()
    handler = LivelogLoggingHandler(str(tmp_path / "log.jsonl"))
    stream = LivelogStdoutHandler(False, parent, handler)
    stream.write("crlf line\r\n")
    assert [e["message"] for e in _stdout_entries(tmp_path)] == ["crlf line"]


def test_create_log_name_length_threshold():
    assert _create_log_name("x" * 110) == "x" * 110 + ".log.jsonl"  # 110 is not truncated
    truncated = _create_log_name("x" * 111)
    assert truncated != "x" * 111 + ".log.jsonl"  # 111 is truncated + hashed
    # truncation caps the length: a much longer id yields the same-length name
    assert len(_create_log_name("x" * 500)) == len(truncated)


def test_create_log_name_long_and_special_combined():
    nodeid = "a/" + "x" * 200  # both filesystem-special and over-long
    name = _create_log_name(nodeid)
    assert name.endswith(".log.jsonl")
    assert "/" not in name
    assert len(name) < len(nodeid)
    assert name != _create_log_name("b/" + "x" * 200)  # stays unique
