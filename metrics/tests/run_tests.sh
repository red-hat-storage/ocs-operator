#!/bin/bash
set -e

alertTestDir="$(cd "$(dirname "$0")" && pwd)"
metricsDir="$alertTestDir/.."
metricsDir="$(cd "$metricsDir" && pwd)"

echo "Alert test directory: $alertTestDir"
echo "Metrics directory: $metricsDir"

(cd "$alertTestDir" && go build -o test_alerts .)

[ ! -f "$alertTestDir/test_alerts" ] && echo "[ERROR] Unable to find the executable" && exit 1
[ ! -x "$alertTestDir/test_alerts" ] && \
    echo "[ERROR] 'test_alerts' is not an executable file" && exit 1

"$alertTestDir"/test_alerts -metrics-dir="$metricsDir" \
    -test-templates="$alertTestDir/templates, $alertTestDir/templates-external" -verbose "$@"
ret_val="$?"

# if extra arguments are passed to the program, don't run the external mode tests
# externa mode tests will be run on default mode
[[ -n "$*" ]] && exit "$ret_val"

# on a default run add the external mode alert tests as well
# running external mode alert tests
"$alertTestDir"/test_alerts -metrics-dir="$metricsDir" -rule-files="$metricsDir/deploy/prometheus-ocs-rules-external.yaml" \
    -test-templates="$alertTestDir/templates-external" -verbose
