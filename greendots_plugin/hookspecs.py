from __future__ import annotations

import pytest


@pytest.hookspec(firstresult=True)
def pytest_greendots_row_params(
    session: pytest.Session, row_params: list[str]
) -> list[str] | None:
    """
    Override the params shown as rows in the greendots plan.

    By default the row params are the params shared by every collected test
    (sorted), which is often empty when tests are parametrized differently.
    Implement this hook (e.g. in a ``conftest.py``) to choose them manually.

    :param session: the pytest session, with ``session.items`` already collected
    :param row_params: the default row params
    :return: the row params to use, in display order, or None to keep the defaults.
        Tests that don't have one of the returned params are shown as "common"
        for it. The first non-None result is used.
    """
