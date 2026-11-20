# GCP provider e2e tests

Isolated OTE module for the non-UEFI Shielded VM and Spot tests. Uses shared
`cluster-api-actuator-pkg/pkg/framework` helpers, not shared test registrations.

## Run locally

Requires an OpenShift GCP cluster with a working Machine API provider and a MAPI
worker MachineSet. CAPI-only GCP workers are not supported by the shared helpers.
Provider credentials must allow access to the non-UEFI image fixture, and the
worker zone must have Spot capacity. The suite is serial and disruptive, with no
conformance parent.

From the repository root; no image or separate binary-build step is needed:

```bash
export KUBECONFIG=/path/to/kubeconfig
mkdir -p /tmp/gcp-provider-results
(
  cd test/e2e
  GOWORK=off CGO_ENABLED=0 go run -trimpath ./cmd \
    run-suite machine-api-provider-gcp/periodic \
    --junit-path /tmp/gcp-provider-results/junit.xml
)
```

Replace `run-suite ...` with `list tests` or `list suites` for cluster-free
discovery. `-trimpath` keeps code locations consistent with the packaged binary.

To build and run with console artifact capture instead:

```bash
ARTIFACT_DIR=/tmp/gcp-provider-results NO_DOCKER=1 make test-e2e-provider
```

Tests create separate MachineSets and clean them up after collecting failure
diagnostics. Allow time for cleanup; a hard process kill can leave resources.

## Development

From the repository root:

```bash
NO_DOCKER=1 make verify-test-extension
```

Metadata verification needs no live cluster. After changing specs, run
`NO_DOCKER=1 make update-test-extension` and include the generated
`.openshift-tests-extension/` changes. Update dependencies
with `make vendor-test-extension`; keep the test module and its Ginkgo replacement
separate from production dependencies.

## CI packaging and artifacts

`Dockerfile.rhel` embeds the static extension in `gcp-machine-controllers` at
`/usr/bin/machine-api-provider-gcp-tests-ext.gz`. There is no separate test image.

Origin registration in `pkg/test/extensions/binary.go` is deferred. Until then,
CI sets `GCP_MACHINE_CONTROLLERS_IMAGE` to the PR-built provider image and runs
`hack/test-e2e-provider.sh` to extract and execute it. Extraction requires `oc`,
`gzip` and image pull access. Without that variable, the script uses
`TEST_EXTENSION_BINARY` or `bin/machine-api-provider-gcp-tests-ext`.

The script saves JUnit, OTE JSON results and `execution.log` under `ARTIFACT_DIR`
(falling back to `JUNIT_DIR`, then `_out/provider-e2e`) and preserves failure exit
status.

Source registrations stay in `cluster-api-actuator-pkg` until destination GCP CI
execution, cleanup and reporting are verified. Shared and webhook tests remain
there.
