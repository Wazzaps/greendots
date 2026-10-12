#!/bin/sh
# Records the output of the real greendots pytest plugin (plan, status files
# and test logs) for suite/sample_tests.py into projects/, for the server's
# plugin contract tests (plugin_contract_test.go). Rerun it when the plugin's
# output format changes, then update the tests to match.
set -eu
cd "$(dirname "$0")"
rm -rf projects

# Some of the sample tests fail on purpose, so pytest must exit with 1
status=0
uv run --project ../../.. pytest suite/sample_tests.py --rootdir . -p no:cacheprovider \
    -n 2 --livelog projects/sample-project/sample-run || status=$?
if [ "$status" -ne 1 ]; then
    echo "expected pytest to exit with 1 (tests failed), got $status" >&2
    exit 1
fi
