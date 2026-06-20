#!/bin/bash

set -euo pipefail

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# shellcheck source=test/scripts/e2e-common.sh
source "${DIR}/e2e-common.sh"

# Set trap only for interruption signals.
# Normally kind cluster cleanup is done by AfterSuite; this suite always owns its
# kind cluster, so an interrupt deletes it.
trap 'e2e_handle_interrupt "e2e-tests"' INT TERM

echo "Running end to end tests"

run_ginkgo_suite "${DIR}/../e2e/"
