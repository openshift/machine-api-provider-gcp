#!/bin/bash

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK_DIR=$(mktemp -d)
trap 'rm -rf "${WORK_DIR}"' EXIT

# Seed the previous metadata so OTE also detects untracked renames/removals.
cp -R "${REPO_ROOT}/.openshift-tests-extension" "${WORK_DIR}/.openshift-tests-extension"
(cd "${WORK_DIR}" && "${REPO_ROOT}/bin/machine-api-provider-gcp-tests-ext" update)
diff -ru "${REPO_ROOT}/.openshift-tests-extension" "${WORK_DIR}/.openshift-tests-extension"
