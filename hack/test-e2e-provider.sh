#!/bin/bash

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TEST_EXTENSION_BINARY=${TEST_EXTENSION_BINARY:-"${REPO_ROOT}/bin/machine-api-provider-gcp-tests-ext"}
OUTPUT_DIR=${ARTIFACT_DIR:-${JUNIT_DIR:-"${REPO_ROOT}/_out/provider-e2e"}}
mkdir -p "${OUTPUT_DIR}"

# Temporary CI route until openshift/origin registers this payload extension.
# Extract from the same provider image included in the PR-built payload, rather
# than maintaining a separate test image. oc uses REGISTRY_AUTH_FILE if supplied.
if [[ -n "${GCP_MACHINE_CONTROLLERS_IMAGE:-}" ]]; then
    WORK_DIR=$(mktemp -d)
    trap 'rm -rf "${WORK_DIR}"' EXIT
    oc image extract "${GCP_MACHINE_CONTROLLERS_IMAGE}" \
        --path="/usr/bin/machine-api-provider-gcp-tests-ext.gz:${WORK_DIR}" --confirm
    TEST_EXTENSION_BINARY="${WORK_DIR}/machine-api-provider-gcp-tests-ext"
    gzip -dc "${TEST_EXTENSION_BINARY}.gz" > "${TEST_EXTENSION_BINARY}"
    chmod u+x "${TEST_EXTENSION_BINARY}"
fi

# run-suite writes JUnit and a separate JSON result file even when tests fail.
# Keep its combined console output as well, including pre-cleanup diagnostics.
"${TEST_EXTENSION_BINARY}" run-suite machine-api-provider-gcp/periodic \
    --junit-path "${OUTPUT_DIR}/junit_machine_api_provider_gcp_e2e.xml" \
    "$@" 2>&1 | tee "${OUTPUT_DIR}/execution.log"
