"""Tests run by ../regenerate.sh to record real greendots plugin output.

The server's tests check what they see through the API against these tests,
so keep the two in sync. (Named so pytest doesn't collect it on its own.)
"""

import logging

import pytest

log = logging.getLogger("sample")


@pytest.mark.parametrize("variant", [1, 2])
@pytest.mark.parametrize("arch", ["x86", "arm"])
def test_matrix(arch, variant, live_progress):
    log.info("checking %s variant %d <b>not bold</b> & co", arch, variant)
    print(f"stdout from {arch}-{variant}")
    live_progress(0.5)
    live_progress.done()
    assert not (arch == "arm" and variant == 2), "arm variant 2 is broken"


@pytest.mark.skip(reason="not supported here")
def test_skipped():
    pass


@pytest.fixture
def broken_fixture():
    raise RuntimeError("fixture setup failed")


def test_setup_error(broken_fixture):
    pass


@pytest.mark.parametrize("path", ["a/b", "c<d>"])
def test_special_chars(path):
    log.warning("path is %s", path)
