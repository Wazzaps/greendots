from __future__ import annotations

import base64
import contextlib
import hashlib
import io
import json
import logging
import math
import os.path
import sys
import threading
import time
import warnings
from collections.abc import Generator, Iterable, Iterator
from typing import Any, TextIO

import pytest


class LivelogLoggingHandler(logging.Handler):
    def __init__(self, path: str) -> None:
        super().__init__()
        self._log_file = open(path, "w")
        self._lock = threading.RLock()
        self.setLevel(logging.DEBUG)
        self.setFormatter(logging.Formatter("%(message)s"))

    def write_log(self, value: dict[str, Any]) -> None:
        with self._lock:
            json.dump(value, self._log_file)
            self._log_file.write("\n")

        self._log_file.flush()

    def emit(self, record: logging.LogRecord) -> None:
        message = self.format(record)
        self.write_log(
            {
                "name": record.name,
                "level": record.levelname,
                "time": record.created,
                "message": message,
            }
        )


class StatusFile:
    def __init__(self, status_file: TextIO) -> None:
        self._status_file = status_file
        self._lock = threading.RLock()

    def log(self, d: dict[str, Any]) -> None:
        if "time" not in d:
            d["time"] = time.time()

        with self._lock:
            json.dump(d, self._status_file)
            self._status_file.write(os.linesep)

        self._status_file.flush()


class ProgressLogger:
    def __init__(
        self,
        status_file: StatusFile | None,
        test: str,
        parent: ProgressLogger | None = None,
        base_value: float = 0,
        fraction: float = 1,
    ) -> None:
        self._status_file = status_file
        self._done = status_file is None
        self._test = test

        # fraction
        self._fraction = fraction
        self._base_value = base_value
        self._last_value = base_value

        # the parent
        self._parent = parent

    def split(self, count: int) -> list[ProgressLogger]:
        """
        Splits the current progress bar into multiple
        smaller ones of equal weights
        """
        loggers = []
        # each child owns an equal slice of *this* bar's 0..1 range; the
        # parent-forwarding in __call__/done() composes the slices back together
        fraction = 1 / count
        for i in range(count):
            loggers.append(
                ProgressLogger(self._status_file, self._test, self, fraction * i, fraction)
            )
        return loggers

    def __call__(self, percentage: float) -> None:
        """
        Update the progress bar with the given percentage
        """

        # if already done ignore
        if self._done:
            return

        # if we have a parent update the parent
        if self._parent is not None:
            self._parent(self._base_value + percentage * self._fraction)
        else:
            # clamp between 0 and 1; snap near-1.0 (float drift from split()) to done
            percentage = min(max(percentage, 0.0), 1.0)
            if percentage >= 1.0 or math.isclose(percentage, 1.0):
                percentage = 1.0
                self._done = True

            # log on >1% change, and always emit the final 100% so the bar completes
            should_log = self._done or abs(percentage - self._last_value) > (1 / 100)

            if should_log:
                # a root logger always has a status file (else it starts done)
                if self._status_file is None:
                    raise RuntimeError("progress logged without a status file")
                self._status_file.log(
                    {"type": "progress", "percentage": percentage, "test": self._test}
                )
                self._last_value = percentage

    def sleep(self, total_seconds: float) -> None:
        """
        Fill the progress bar by sleeping
        """
        # no-op if the logger is disabled (no livelog) or already complete
        if self._done:
            return

        # we only need to update every 1%, so let it sleep for a good amount
        # of time between each, technically this might still be too much if we
        # are just a child of something else, but this is good enough for our needs
        update_interval = total_seconds / 100

        # fill based on elapsed time; a child never flips its own `_done`, so
        # terminate on the elapsed fraction rather than relying on it
        start_time = time.time()
        while not self._done:
            elapsed = time.time() - start_time
            fraction = elapsed / total_seconds if total_seconds > 0 else 1.0
            self(min(fraction, 1.0))
            if fraction >= 1.0:
                break
            time.sleep(min(update_interval, total_seconds - elapsed))

    def done(self) -> None:
        if self._done:
            return

        self._done = True

        # a child reports completion (the whole of its slice) up to its parent;
        # a root writes the final 100% line itself
        if self._parent is not None:
            self._parent(self._base_value + self._fraction)
        else:
            # not-done implies the logger was created with a status file
            if self._status_file is None:
                raise RuntimeError("progress completed without a status file")
            self._status_file.log({"type": "progress", "percentage": 1.0, "test": self._test})


def json_encode_default(o: object) -> Any:
    f = getattr(o, "__livelog_format__", None)
    if f is None:
        return repr(o)
    else:
        return f()


class LivelogStdoutHandler(io.TextIOBase):
    def __init__(self, error: bool, parent: TextIO, handler: LivelogLoggingHandler) -> None:
        super().__init__()
        self._error = error
        self._last_line: str | None = None
        self._handler = handler
        self._parent = parent

    def _log(self, message: str) -> None:
        # we don't use a normal logger since we want the stdout to stay as stdout
        # as far as pytest is concerned, so just let the emit work its magic
        self._handler.write_log(
            {
                "name": "stderr" if self._error else "stdout",
                "level": "ERROR" if self._error else "INFO",
                "time": time.time(),
                "message": message,
            }
        )

    def write(self, s: str) -> int:
        self._parent.write(s)

        for line in s.splitlines(keepends=True):
            if line.endswith("\n"):
                if self._last_line is not None:
                    line = self._last_line + line
                    self._last_line = None
                self._log(line.rstrip("\r\n"))
            else:
                # accumulate consecutive partial fragments until a newline arrives
                self._last_line = (self._last_line or "") + line
        return len(s)

    def flush_partial(self) -> None:
        """Emit any buffered trailing text that never received a newline."""
        if self._last_line is not None:
            self._log(self._last_line)
            self._last_line = None

    # typeshed types IOBase.writelines for binary buffers; text streams take str.
    def writelines(self, lines: Iterable[str]) -> None:  # type: ignore[override]
        self.write("".join(lines))


def pytest_addoption(parser: pytest.Parser) -> None:
    parser.addoption(
        "--livelog",
        help="Sets the path the logs, plan and status files should be written to",
    )
    parser.addoption(
        "--livelog-worker-id",
        default=None,
        help="Get the worker id in line instead of from the xdist.",
        type=int,
    )
    parser.addoption(
        "--livelog-worker-count",
        default=None,
        help="Override the number of workers in the entire run.",
        type=int,
    )
    parser.addoption(
        "--skip-plan-creation",
        default=False,
        action="store_true",
        help="Override livelog to not create a plan.json but use an existing one.",
    )


_FILENAME_SPECIAL_CHARS = '<>"/\\|?*'


def _hash(s: str) -> str:
    digest = base64.b32encode(hashlib.blake2b(s.encode("utf-8"), digest_size=5).digest()).decode(
        "utf-8"
    )
    return digest


def _create_log_name(nodeid: str) -> str:
    # handle long file names properly
    log_file_name = nodeid

    # check if has special symbols
    has_special_symbols = False
    for c in _FILENAME_SPECIAL_CHARS:
        if c in log_file_name:
            has_special_symbols = True

    # if we have special symbols we will suffix the
    # file with a hash to make sure the mangling won't
    # create name collisions, and we want to make sure we
    # account for that in the filename
    length = len(log_file_name)
    if has_special_symbols:
        length += 9

    # check for filename being too long, truncate it if so
    # and add a hash to make it unique
    if length > 110:
        begin = log_file_name[:50]
        end = log_file_name[-50:]
        digest = _hash(nodeid)
        log_file_name = f"{begin}-{digest}-{end}"

    # if we have special character append the digest to make
    # sure we won't have a name collision
    elif has_special_symbols:
        digest = _hash(nodeid)
        log_file_name = f"{log_file_name}-{digest}"

    # replace special characters
    if has_special_symbols:
        for c in _FILENAME_SPECIAL_CHARS:
            log_file_name = log_file_name.replace(c, "_")

    # and lastly add the extension
    return log_file_name + ".log.jsonl"


class LivelogPlugin:
    def __init__(self) -> None:
        self._log_path: str | None = None
        self._worker_id: int | None = None
        self._worker_count: int | None = None
        self._skip_plan = False

        self._status_file: StatusFile | None = None
        self._handler: LivelogLoggingHandler | None = None

        self._worst_outcome: str | None = None

        self._xdist_supported = False

    def pytest_configure(self, config: pytest.Config) -> None:
        self._log_path = config.getoption("livelog")
        if self._log_path is None:
            return

        self._worker_id = config.getoption("--livelog-worker-id")
        self._worker_count = config.getoption("--livelog-worker-count")
        self._skip_plan = config.getoption("--skip-plan-creation")

        # I don't care if we create on everything including
        # the workers, it doesn't change anything
        os.makedirs(self._log_path, exist_ok=True)

        # make sure to mark we have xdist loaded
        if config.pluginmanager.hasplugin("xdist"):
            self._xdist_supported = True

    def is_worker(self, session: pytest.Session) -> bool:
        if self._log_path is None:
            return False

        if self._xdist_supported:
            import xdist

            if xdist.is_xdist_controller(session) or xdist.is_xdist_master(session):
                return False

        return True

    def get_worker_id(self, session: pytest.Session) -> int:
        if isinstance(self._worker_id, int):
            return self._worker_id

        elif self._xdist_supported:
            import xdist

            if xdist.is_xdist_worker(session):
                xdist_worker_id: str = xdist.get_xdist_worker_id(session)
                if not xdist_worker_id.startswith("gw"):
                    raise ValueError(f"Invalid worker id {xdist_worker_id}")
                return int(xdist_worker_id[len("gw") :])

        return 0

    def pytest_sessionstart(self, session: pytest.Session) -> None:
        if not self.is_worker(session):
            return

        # is_worker() returning True guarantees a log path was configured
        if self._log_path is None:
            raise RuntimeError("worker session started without a log path")
        self._status_file = StatusFile(
            open(
                os.path.join(self._log_path, f"status.{self.get_worker_id(session)}.jsonl"),
                "a+",
            )
        )

    def pytest_runtest_logreport(self, report: pytest.TestReport) -> None:
        if self._status_file is None:
            return

        d: dict[str, Any] = {
            "type": report.when,
            "outcome": report.outcome,
            "test": report.nodeid,
        }

        if report.outcome == "failed":
            d["exception"] = report.longreprtext
            self._worst_outcome = "failed"
            # mirror the failure into the per-test log (no active exception here,
            # so log the text rather than using logging.exception)
            logging.error(report.longreprtext)

        elif report.outcome == "skipped":
            # for skipped tests longrepr is a (path, lineno, reason) tuple
            longrepr = report.longrepr
            reason = longrepr[2] if isinstance(longrepr, tuple) else str(longrepr)
            d["reason"] = reason
            logging.warning(reason)

            if self._worst_outcome == "passed":
                self._worst_outcome = "skipped"

        self._status_file.log(d)

    def should_create_plan(self, session: pytest.Session) -> bool:
        if self._skip_plan or self._log_path is None:
            return False
        elif self._xdist_supported:
            import xdist

            is_controller = xdist.is_xdist_controller(session)
            is_master = xdist.is_xdist_master(session)
            is_worker = xdist.is_xdist_worker(session)

            if is_master or is_controller:
                return False

            if is_worker:
                worker_id: str = xdist.get_xdist_worker_id(session)
                return worker_id == "gw0"
        return True

    @property
    def worker_count(self) -> int:
        if isinstance(self._worker_count, int):
            return self._worker_count

        if self._xdist_supported:
            return int(os.getenv("PYTEST_XDIST_WORKER_COUNT", "1"))

        return 1

    def pytest_collection_finish(self, session: pytest.Session) -> None:
        if not self.should_create_plan(session):
            return

        # should_create_plan() returning True guarantees a log path was configured
        if self._log_path is None:
            raise RuntimeError("plan creation requested without a log path")

        # go over the items and generate the plan
        groups: dict[str, list[dict[str, Any]]] = {}
        row_params: set[str] | None = None
        for item in session.items:
            if isinstance(item, pytest.Function):
                nodeid = item.nodeid
                test_name = item.obj.__name__
                test_module = item.obj.__module__
                callspec = getattr(item, "callspec", None)
                if callspec is not None:
                    test_params = callspec.params
                else:
                    test_params = dict()

                if row_params is None:
                    row_params = set(test_params.keys())
                else:
                    row_params.intersection_update(set(test_params.keys()))

            else:
                # non-Function items (doctests, custom collectors, ...) don't fit
                # the test-matrix model; skip them but warn so it isn't a silent drop
                warnings.warn(
                    f"greendots: skipping non-Function collection item {item!r}",
                    stacklevel=2,
                )
                continue

            if test_module not in groups:
                groups[test_module] = []

            groups[test_module].append(
                {
                    "id": nodeid,
                    "log_file": _create_log_name(nodeid),
                    "name": test_name,
                    "params": test_params,
                }
            )

        plan = {
            "worker_count": self.worker_count,
            "groups": groups,
            # sorted so plan.json is deterministic across runs (row_params is a set)
            "row_params": [] if row_params is None else sorted(row_params),
        }

        # write the plan to a file
        with open(os.path.join(self._log_path, "plan.json"), "w") as f:
            json.dump(plan, f, sort_keys=True, default=json_encode_default, indent=4)

    def pytest_runtest_logstart(self, nodeid: str, location: tuple[str, int | None, str]) -> None:
        # we are not a worker, ignore
        if self._status_file is None:
            return

        if self._handler is not None:
            raise RuntimeError("pytest_runtest_logstart called before pytest_runtest_logfinish")

        # a status file is only opened for workers, which always have a log path
        if self._log_path is None:
            raise RuntimeError("worker log handler created without a log path")
        log_file = os.path.join(self._log_path, _create_log_name(nodeid))

        # open the handler and set it
        self._handler = LivelogLoggingHandler(log_file)

        # add our handler and make sure that the logging
        # level is DEBUG so we capture everything
        root_logger = logging.getLogger()
        root_logger.addHandler(self._handler)
        root_logger.setLevel(logging.DEBUG)

        self._worst_outcome = "passed"

        self._status_file.log({"type": "start", "test": nodeid})

    def pytest_runtest_logfinish(self, nodeid: str, location: tuple[str, int | None, str]) -> None:
        if self._status_file is None:
            return

        self._status_file.log({"type": "finish", "outcome": self._worst_outcome, "test": nodeid})

        # we can remove the handler and close it
        # since no one should be using it anymore
        if self._handler is None:
            raise RuntimeError("pytest_runtest_logfinish called before pytest_runtest_logstart")
        logging.getLogger().removeHandler(self._handler)
        self._handler = None

        self._worst_outcome = None

    #
    # For each phase we will also replace the stdout/stderr
    # this is done as last as possible so we will do it after
    # pytest captures the stdout/stderr, we will pass to it
    # everything as normal but we will just log them into our
    # file as well
    #

    @contextlib.contextmanager
    def _capture_output(self) -> Iterator[None]:
        if self._handler is None:
            raise RuntimeError("output capture started without a log handler")

        original_stdout = sys.stdout
        original_stderr = sys.stderr
        stdout_handler = LivelogStdoutHandler(False, original_stdout, self._handler)
        stderr_handler = LivelogStdoutHandler(True, original_stderr, self._handler)
        sys.stdout = stdout_handler
        sys.stderr = stderr_handler

        try:
            yield
        finally:
            # emit any trailing partial lines before restoring the real streams
            stdout_handler.flush_partial()
            stderr_handler.flush_partial()
            sys.stdout = original_stdout
            sys.stderr = original_stderr

    @pytest.hookimpl(trylast=True, wrapper=True)
    def pytest_runtest_setup(self, item: pytest.Item) -> Generator[None, object, object]:
        __tracebackhide__ = True

        if not self._validate_worker():
            return (yield)

        with self._capture_output():
            return (yield)

    @pytest.hookimpl(trylast=True, wrapper=True)
    def pytest_runtest_call(self, item: pytest.Item) -> Generator[None, object, object]:
        __tracebackhide__ = True

        if not self._validate_worker():
            return (yield)

        with self._capture_output():
            return (yield)

    @pytest.hookimpl(trylast=True, wrapper=True)
    def pytest_runtest_teardown(self, item: pytest.Item) -> Generator[None, object, object]:
        __tracebackhide__ = True

        if not self._validate_worker():
            return (yield)

        with self._capture_output():
            return (yield)

    def _validate_worker(self) -> bool:
        """
        Validates the worker: returns True if log capturing should run, False if
        it should be skipped (no livelog), and raises if the worker is in an
        inconsistent state.
        """
        if self._log_path is None:
            return False

        if self._handler is None:
            raise RuntimeError("Didn't create log handler for worker")
        if self._status_file is None:
            raise RuntimeError("Didn't create status file for worker")
        return True

    @pytest.fixture
    def live_progress(self, request: pytest.FixtureRequest) -> ProgressLogger:
        return ProgressLogger(self._status_file, request.node.nodeid)


def pytest_configure(config: pytest.Config) -> None:
    config.pluginmanager.register(LivelogPlugin())
