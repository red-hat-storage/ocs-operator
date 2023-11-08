#!/bin/bash
set -e

alertTestDir="$(cd "$(dirname "$0")" && pwd)"
[ ! -d "$alertTestDir/templates" ] && \
    echo "[ERROR] Sorry unable to find the directory containing template files" && exit 1

mixinDir="$alertTestDir/.."
mixinDir="$(cd "$mixinDir" && pwd)"

echo "Alert test directory: $alertTestDir"
echo "Mixin directory: $mixinDir"

(cd "$alertTestDir" && go build -o test_alerts .)

[ ! -f "$alertTestDir/test_alerts" ] && echo "[ERROR] Unable to find the executable" && exit 1
[ ! -x "$alertTestDir/test_alerts" ] && \
    echo "[ERROR] 'test_alerts' is not an executable file" && exit 1

"$alertTestDir"/test_alerts -mixin-path="$mixinDir" \
    -test-templates="$alertTestDir/templates, $alertTestDir/templates-external" -verbose "$@"
ret_val="$?"

# if extra arguments are passed to the program, don't run the external mode tests
# externa mode tests will be run on default mode
[[ -n "$*" ]] && exit "$ret_val"

# on a default run add the external mode alert tests as well
# running external mode alert tests
"$alertTestDir"/test_alerts -mixin-path="$mixinDir" -rule-files="$mixinDir/build/external.jsonnet" \
    -test-templates="$alertTestDir/templates-external" -verbose
