import io
import json
import logging
import os.path
import sys
import threading
import time
import hashlib
import base64
from typing import Iterable

import pytest


class LivelogLoggingHandler(logging.Handler):
    def __init__(self, path):
        super().__init__()
        self._log_file = open(path, "w")
        self._lock = threading.RLock()
        self.setLevel(logging.DEBUG)
        self.setFormatter(logging.Formatter("%(message)s"))

    def write_log(self, value):
        with self._lock:
            json.dump(value, self._log_file)
            self._log_file.write("\n")

        self._log_file.flush()

    def emit(self, record: logging.LogRecord):
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
    def __init__(self, status_file):
        self._status_file = status_file
        self._lock = threading.RLock()

    def log(self, d):
        if "time" not in d:
            d["time"] = time.time()

        with self._lock:
            json.dump(d, self._status_file)
            self._status_file.write(os.linesep)

        self._status_file.flush()


class ProgressLogger:
    def __init__(self, status_file: StatusFile, test, parent: 'ProgressLogger' = None, base_value: float = 0, fraction: float = 1):
        self._status_file = status_file
        self._done = status_file is None
        self._test = test
        
        # fraction
        self._fraction = fraction
        self._base_value = base_value
        self._last_value = base_value

        # the parent         
        self._parent = parent
    
    def split(self, count):
        """
        Splits the current progress bar into multiple 
        smaller ones of equal weights
        """
        loggers = []
        fraction = self._fraction / count
        for i in range(count):
            loggers.append(ProgressLogger(self._status_file, self._done, self, fraction * i, fraction))
        return loggers

    def __call__(self, percentage):
        """
        Update the progress bar with the given percentage
        """
        
        # if already done ignore
        if self._done:
            return

        # if we have a parent update the parent
        if self._parent is not None:
            self._parent(self._base_value + self._value * self._fraction)
        else:
            # clamp between 0 and 1
            percentage = min(max(percentage, 0.0), 1.0)
            if percentage >= 1.0:
                self._done = True
            
            # make sure we only log if we have a difference of 1% of change
            should_log = abs(percentage - self._last_value) > (1 / 100)
            
            if should_log:
                self._status_file.log(
                    {"type": "progress", "percentage": percentage, "test": self._test}
                )
                self._last_value = self._last_value
                
    def sleep(self, total_seconds):
        """
        Fill the progress bar by sleeping
        """
        assert not self._done, "Progress logger is already done"
        
        # we only need to update every 1%, so let it sleep for a good amount 
        # of time between each, technically this might still be too much if we 
        # are just a child of something else, but this is good enough for our needs
        update_interval = total_seconds / 100
        
        # go in a loop until we are done 
        start_time = time.time()
        while True:
            elapsed = time.time() - start_time
            self(elapsed / total_seconds)
            if self._done:
                break
            time.sleep(min(update_interval, total_seconds - elapsed))

    def done(self):
        if self._done:
            return

        self._done = True
        self._status_file.log({"type": "progress", "percentage": 1.0})


def json_encode_default(o):
    f = getattr(o, '__livelog_format__', None)
    if f is None:
        return repr(o)
    else:
        return f()


class LivelogStdoutHandler(io.TextIOBase):
    def __init__(self, error, parent, handler: LivelogLoggingHandler):
        super().__init__()
        self._error = error
        self._last_line = None
        self._handler = handler
        self._parent = parent

    def write(self, s: str) -> int:
        self._parent.write(s)

        lines = s.splitlines(keepends=True)
        for line in lines:
            if line.endswith("\n"):
                if self._last_line is not None:
                    line = self._last_line + line
                    self._last_line = None

                # we don't use a normal logger since we want the stdout to stay as stdout
                # as far as pytest is concerned, so just let the emit work its magic
                self._handler.write_log(
                    {
                        "name": "stderr" if self._error else "stdout",
                        "level": "ERROR" if self._error else "INFO",
                        "time": time.time(),
                        "message": line[:-1],
                    }
                )
            else:
                self._last_line = line
        return len(s)

    def writelines(self, lines: Iterable[str]):
        self.write("".join(lines))


def pytest_addoption(parser):
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


def _hash(s):
    digest = base64.b32encode(hashlib.blake2b(s.encode('utf-8'), digest_size=5).digest()).decode('utf-8')
    return digest


def _create_log_name(nodeid):

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
        log_file_name = f'{begin}-{digest}-{end}'

    # if we have special character append the digest to make
    # sure we won't have a name collision
    elif has_special_symbols:
        digest = _hash(nodeid)
        log_file_name = f'{log_file_name}-{digest}'

    # replace special characters
    if has_special_symbols:
        for c in _FILENAME_SPECIAL_CHARS:
            log_file_name = log_file_name.replace(c, '_')

    # and lastly add the extension
    return log_file_name + '.log.jsonl'


class LivelogPlugin:
    def __init__(self):
        self._log_path = None
        self._worker_id = None
        self._worker_count = None
        self._skip_plan = False

        self._status_file: StatusFile = None
        self._handler = None
        self._stdout = None
        self._stderr = None

        self._worst_outcome = None

        self._xdist_supported = False

    def pytest_configure(self, config: pytest.Config):
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
                xdist_worker_id = xdist.get_xdist_worker_id(session)
                assert xdist_worker_id.startswith('gw'), f"Invalid worker id {xdist_worker_id}"
                return int(xdist_worker_id[len('gw'):])

        return 0

    def pytest_sessionstart(self, session):
        if not self.is_worker(session):
            return

        self._status_file = StatusFile(status_file=open(os.path.join(self._log_path, f"status.{self.get_worker_id(session)}.jsonl"), "a+"))

    def pytest_runtest_logreport(self, report: pytest.TestReport):
        if self._status_file is None:
            return

        d = {
            "type": report.when,
            "outcome": report.outcome,
            "test": report.nodeid,
        }

        if report.outcome == "failed":
            d["exception"] = report.longreprtext
            self._worst_outcome = "failed"
            logging.exception(report.longrepr)

        elif report.outcome == "skipped":
            d["reason"] = report.longrepr[2]
            logging.warning(report.longrepr[2])

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
                worker_id = xdist.get_xdist_worker_id(session)
                return worker_id == 'gw0'
        return True

    @property
    def worker_count(self) -> int:
        if isinstance(self._worker_count, int):
            return self._worker_count

        if self._xdist_supported:
            return int(os.getenv("PYTEST_XDIST_WORKER_COUNT", 1))

        return 1

    def pytest_collection_finish(self, session: pytest.Session):
        if not self.should_create_plan(session):
            return

        # go over the items and generate the plan
        groups = {}
        row_params = None
        for item in session.items:
            if isinstance(item, pytest.Function):
                logging.info(item.own_markers)

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
                assert False, f"Invalid pytest session item type {type(item)}"

            if test_module not in groups:
                groups[test_module] = []

            groups[test_module].append(
                {"id": nodeid, "log_file": _create_log_name(nodeid), "name": test_name, "params": test_params}
            )

        plan = {
            "worker_count": self.worker_count,
            "groups": groups,
            "row_params": [] if row_params is None else list(row_params),
        }

        # write the plan to a file
        with open(os.path.join(self._log_path, "plan.json"), "w") as f:
            json.dump(plan, f, sort_keys=True, default=json_encode_default, indent=4)

    def pytest_runtest_logstart(self, nodeid, location):
        # we are not a worker, ignore
        if self._status_file is None:
            return

        assert self._handler is None, "pytest_runtest_logstart called before pytest_runtest_logfinish"

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

    def pytest_runtest_logfinish(self, nodeid, location):
        if self._status_file is None:
            return

        self._status_file.log(
            {"type": "finish", "outcome": self._worst_outcome, "test": nodeid}
        )

        # we can remove the handler and close it
        # since no one should be using it anymore
        assert self._handler is not None, "pytest_runtest_logfinish called before pytest_runtest_logstart"
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

    @pytest.hookimpl(trylast=True, wrapper=True)
    def pytest_runtest_setup(self, item):
        __tracebackhide__ = True

        if not self._validate_worker():
            return (yield)

        self._stdout = sys.stdout
        self._stderr = sys.stderr

        sys.stdout = LivelogStdoutHandler(False, self._stdout, self._handler)
        sys.stderr = LivelogStdoutHandler(True, self._stderr, self._handler)

        try:
            return (yield)
        finally:
            sys.stdout = self._stdout
            sys.stderr = self._stderr

    @pytest.hookimpl(trylast=True, wrapper=True)
    def pytest_runtest_call(self, item):
        __tracebackhide__ = True

        if not self._validate_worker():
            return (yield)

        self._stdout = sys.stdout
        self._stderr = sys.stderr

        sys.stdout = LivelogStdoutHandler(False, self._stdout, self._handler)
        sys.stderr = LivelogStdoutHandler(True, self._stderr, self._handler)

        try:
            return (yield)
        finally:
            sys.stdout = self._stdout
            sys.stderr = self._stderr

    @pytest.hookimpl(trylast=True, wrapper=True)
    def pytest_runtest_teardown(self, item):
        __tracebackhide__ = True

        if not self._validate_worker():
            return (yield)

        self._stdout = sys.stdout
        self._stderr = sys.stderr

        sys.stdout = LivelogStdoutHandler(False, self._stdout, self._handler)
        sys.stderr = LivelogStdoutHandler(True, self._stderr, self._handler)

        try:
            return (yield)
        finally:
            sys.stdout = self._stdout
            sys.stderr = self._stderr

    def _validate_worker(self):
        """
        Validates the worker, either asserts
        or return True if should run, if the
        log capturing should not be done
        returns False
        """
        if self._log_path is None:
            return False

        assert self._handler is not None, "Didn't create log handler for worker"
        assert self._status_file is not None, "Didn't create status file for worker"
        return True

    @pytest.fixture
    def live_progress(self, request: pytest.FixtureRequest):
        return ProgressLogger(self._status_file, request.node.nodeid)


def pytest_configure(config):
    config.pluginmanager.register(LivelogPlugin())
