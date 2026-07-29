# Bumping Kubernetes and Go

This document describes how to bump Kubernetes and Go versions across the
project. It is primarily intended to be consumed by an AI coding agent (e.g. via
`/bump-k8s-go 1.36 1.26`), but the steps can also be followed manually.

The first argument is the target **Kubernetes minor** version (e.g. `1.36`) and
the second is the target **Go minor** version (e.g. `1.26`).

## Prerequisites

This repository depends on several OpenShift repositories that must be bumped
**before** this one. Each prerequisite repository must have a merged PR that
targets the same Kubernetes and Go versions. Verify all of these before
proceeding:

### openshift/api

The OpenShift API types repository must be bumped first. It defines the shared
API types consumed by all other OpenShift components.

```bash
gh pr list --repo openshift/api --state merged \
  --search "bump k8s ${K8S_MINOR} OR k8s ${K8S_MINOR}" \
  --limit 5
```

Record the commit SHA from the merged PR. Call this `OPENSHIFT_API_COMMIT`.

### openshift/client-go

The generated client library for OpenShift API types must be updated to match
the new `openshift/api`.

```bash
gh pr list --repo openshift/client-go --state merged \
  --search "bump k8s ${K8S_MINOR} OR k8s ${K8S_MINOR}" \
  --limit 5
```

Record the commit SHA. Call this `OPENSHIFT_CLIENT_GO_COMMIT`.

### openshift/library-go

Shared OpenShift library code, which depends on both `openshift/api` and
`openshift/client-go`.

```bash
gh pr list --repo openshift/library-go --state merged \
  --search "bump k8s ${K8S_MINOR} OR k8s ${K8S_MINOR}" \
  --limit 5
```

Record the commit SHA. Call this `OPENSHIFT_LIBRARY_GO_COMMIT`.

### openshift/machine-api-operator (MAO)

The machine-api-operator is the most critical prerequisite. This provider
imports types and test helpers from MAO, and MAO itself depends on the three
repositories above.

**Important:** MAO's default branch is `main`, not `master`. Always use `@main`
when fetching with `go get`.

```bash
gh pr list --repo openshift/machine-api-operator --state merged \
  --search "bump k8s ${K8S_MINOR} OR k8s ${K8S_MINOR}" \
  --limit 5
```

Record the commit SHA (the pseudo-version). Call this `MAO_COMMIT`.

### Envtest assets (openshift/api)

The `unit` Makefile target downloads envtest binaries (etcd, kube-apiserver)
from an OpenShift-maintained index at
[openshift/api/envtest-releases.yaml](https://github.com/openshift/api/blob/master/envtest-releases.yaml).
Assets must be published for the target k8s version before the bump can proceed.

Check availability:

```bash
gh api repos/openshift/api/contents/envtest-releases.yaml \
  --jq '.content' | base64 -d | grep "v${K8S_MINOR}"
```

This should return one or more `v${K8S_MINOR}.X:` entries. The **highest patch
version** listed is what `ENVTEST_K8S_VERSION` in the Makefile should be set to.
Call this `ENVTEST_K8S_VERSION`.

**Important:** The k8s.io module versions in `go.mod` must match this patch
version exactly — not a newer patch that lacks envtest assets. For example, if
the index has `v1.36.2` but not `v1.36.3`, all `k8s.io/*` modules must be at
`v0.36.2`, not `v0.36.3`.

If **no entry exists** for the target k8s minor, **stop**. The envtest assets
must be published first — tests will fail without them. Check with the
openshift/api maintainers or wait for the assets to be added.

### Prerequisite verification

Before continuing, confirm that each prerequisite's `go.mod` targets the
expected `k8s.io/*` version and Go version. A quick check:

```bash
for repo in openshift/api openshift/client-go openshift/library-go openshift/machine-api-operator; do
  echo "=== $repo ==="
  curl -s "https://raw.githubusercontent.com/$repo/main/go.mod" \
    | grep -E '^go |k8s.io/api ' | head -2
done
```

If any prerequisite is not yet bumped, **stop** and bump it first (or wait for
the responsible team to merge their PR).

## Step 1: Research

Perform these lookups before making any changes. Call the first argument
`K8S_MINOR` and the second `GO_MINOR`.

### 1a. Kubernetes patch version

Find the highest patch version available in the envtest index (determined in
Prerequisites). This is the version to use for all `k8s.io/*` modules:

```bash
gh api repos/openshift/api/contents/envtest-releases.yaml \
  --jq '.content' | base64 -d | grep "v${K8S_MINOR}" | tail -1
```

Call this `K8S_PATCH` (e.g. `1.36.2`). The module version is
`v0.${K8S_MINOR##1.}.${K8S_PATCH##*.}` (e.g. `v0.36.2`).

### 1b. controller-runtime version

Find the controller-runtime version that targets the new k8s minor. The mapping
is:

| k8s minor | controller-runtime |
|-----------|--------------------|
| 1.33      | 0.21.x             |
| 1.34      | 0.22.x             |
| 1.35      | 0.23.x             |
| 1.36      | 0.24.x             |

Cross-reference with MAO's `go.mod` to ensure compatibility:

```bash
curl -s "https://raw.githubusercontent.com/openshift/machine-api-operator/main/go.mod" \
  | grep 'sigs.k8s.io/controller-runtime '
```

Call this `CONTROLLER_RUNTIME_VERSION` (e.g. `v0.24.1`).

### 1c. controller-tools version

Check if controller-tools needs bumping:

```bash
gh api repos/kubernetes-sigs/controller-tools/releases \
  --paginate --jq '.[].tag_name' | head -10
```

Call this `CONTROLLER_TOOLS_VERSION`.

### 1d. OpenShift release mapping

Determine the target OpenShift release version that corresponds to this k8s
bump. The mapping is roughly:

| k8s minor | OCP version |
|-----------|-------------|
| 1.28      | 4.15        |
| 1.29      | 4.16        |
| 1.30      | 4.17        |
| 1.31      | 4.18        |
| 1.32      | 4.19        |
| 1.33      | 4.20        |
| 1.34      | 4.21        |
| 1.35      | 4.22        |
| 1.36      | 5.0         |

Call this `OCP_VERSION`. This determines the builder image tags.

### 1e. Builder image availability

Verify that CI builder images exist for the target Go version and OCP release:

```
registry.ci.openshift.org/ocp/builder:rhel-9-golang-${GO_MINOR}-openshift-${OCP_VERSION}
```

Also check the release image tag for `.ci-operator.yaml`:

```
rhel-9-release-golang-${GO_MINOR}-openshift-${OCP_VERSION}
```

And the base image for `Dockerfile.rhel`:

```
registry.ci.openshift.org/ocp/${OCP_VERSION}:base-rhel9
```

### 1f. Breaking changes

Check the controller-runtime and k8s changelogs for breaking changes:

```bash
gh api repos/kubernetes-sigs/controller-runtime/releases \
  --jq ".[] | select(.tag_name | startswith(\"v0.XX\")) | .body" | head -100
```

Also check if MAO's bump PR had any code changes beyond `go.mod`/vendor:

```bash
gh api repos/openshift/machine-api-operator/pulls/<MAO_PR_NUMBER>/files \
  --paginate --jq '.[].filename' | grep -v vendor
```

Note any required code changes.

## Step 2: Update go.mod

### 2a. Update Go version

```bash
go mod edit -go=${GO_MINOR}
```

If the `go.mod` has a `toolchain` directive, evaluate whether it should be
updated or removed. When the `go` directive specifies the exact minor (e.g.
`go 1.26`), a separate `toolchain` line is typically unnecessary.

### 2b. Bump Kubernetes dependencies

```bash
K8S_MOD_VERSION=v0.${K8S_MINOR##1.}.${K8S_PATCH##*.}  # e.g. v0.36.2

go get k8s.io/api@${K8S_MOD_VERSION}
go get k8s.io/apimachinery@${K8S_MOD_VERSION}
go get k8s.io/apiserver@${K8S_MOD_VERSION}
go get k8s.io/client-go@${K8S_MOD_VERSION}
go get k8s.io/component-base@${K8S_MOD_VERSION}
```

### 2c. Bump controller-runtime and controller-tools

```bash
go get sigs.k8s.io/controller-runtime@${CONTROLLER_RUNTIME_VERSION}
go get sigs.k8s.io/controller-tools@${CONTROLLER_TOOLS_VERSION}
```

### 2d. Bump OpenShift dependencies

Use `@main` for MAO (not `@master` — MAO's default branch is `main`):

```bash
go get github.com/openshift/api@${OPENSHIFT_API_COMMIT}
go get github.com/openshift/library-go@${OPENSHIFT_LIBRARY_GO_COMMIT}
go get github.com/openshift/machine-api-operator@main
```

`openshift/client-go` is an indirect dependency — it will be pulled in
transitively via `openshift/machine-api-operator` and `openshift/library-go`.

### 2e. Update setup-envtest

If the controller-runtime minor changed, check if setup-envtest needs a
corresponding update:

```bash
go get sigs.k8s.io/controller-runtime/tools/setup-envtest@${SETUP_ENVTEST_VER}
```

## Step 3: Tidy and vendor

```bash
go mod tidy
go mod vendor
go mod verify
```

If `go mod tidy` fails with a missing package error (e.g.
`k8s.io/api/scheduling/v1alpha1`), it means a transitive dependency still
references a removed API group. Ensure all `k8s.io/*` modules are at the same
minor version and that MAO is on a sufficiently recent commit.

## Step 4: Update build infrastructure

### 4a. Makefile

Update `ENVTEST_K8S_VERSION` to the highest patch version available in the
[openshift/api envtest-releases.yaml](https://github.com/openshift/api/blob/master/envtest-releases.yaml)
index for the target k8s minor (determined in the Prerequisites section):

```makefile
ENVTEST_K8S_VERSION = ${ENVTEST_K8S_VERSION}
```

Update `BUILD_IMAGE` to the new builder image:

```makefile
BUILD_IMAGE ?= registry.ci.openshift.org/ocp/builder:rhel-9-golang-${GO_MINOR}-openshift-${OCP_VERSION}
```

### 4b. Dockerfile

Update the builder image in the `FROM` line:

```dockerfile
FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-${GO_MINOR}-openshift-${OCP_VERSION} AS builder
```

### 4c. Dockerfile.rhel

Update the builder image similarly. Note that `Dockerfile.rhel` may already be
ahead if ART has updated it — check before overwriting:

```dockerfile
FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-${GO_MINOR}-openshift-${OCP_VERSION} AS builder
```

Also update the base image if the OCP version changed:

```dockerfile
FROM registry.ci.openshift.org/ocp/${OCP_VERSION}:base-rhel9
```

### 4d. .ci-operator.yaml

Update the build root image tag:

```yaml
build_root_image:
  name: release
  namespace: openshift
  tag: rhel-9-release-golang-${GO_MINOR}-openshift-${OCP_VERSION}
```

### 4e. AGENTS.md

Update `setup-envtest use` version references in example commands to match
`ENVTEST_K8S_VERSION`. There are typically two occurrences:

```bash
grep -n 'setup-envtest use' AGENTS.md
```

Replace the version number in each match.

## Step 5: Apply code changes

Using the breaking changes identified in Step 1f, apply any required code
changes. This step varies per bump. Common categories from historical bumps
include:

- **Removed API groups**: k8s may graduate or remove alpha/beta API groups
  (e.g. `scheduling/v1alpha1` was removed in 1.36). If transitive deps still
  reference them, all `k8s.io/*` modules must be at the same version.

- **controller-runtime interface changes**: The `ResourceEventHandlerRegistration`
  interface may gain new methods between k8s minors. Bumping controller-runtime
  to the matching version resolves this.

- **CRD directory paths**: If `openshift/api` reorganizes CRD manifests, test
  files referencing `CRDDirectoryPaths` may need path updates. Check:
  - `pkg/cloud/gcp/actuators/machine/actuator_test.go`
  - `pkg/cloud/gcp/actuators/machine/machine_scope_test.go`
  - `pkg/cloud/gcp/actuators/machineset/controller_suite_test.go`
  - `pkg/termination/termination_suite_test.go`

- **Test framework changes**: test helpers from MAO may have changed signatures.

Review the MAO bump diff for guidance on what code changes were needed there.

## Step 6: Validate

```bash
CGO_ENABLED=0 make build   # CGO_ENABLED=0 needed on macOS; CI builds on Linux
make test
make fmt
make vet
```

Note: `make build` uses `-extldflags -static` which the macOS linker does not
support. Use `CGO_ENABLED=0` for local builds, which produces a pure Go binary.

Fix any failures before proceeding.

## Step 7: Commit

Create separate commits for distinct concerns. The typical commit structure for
a bump PR is:

1. **The version bump and build infra** (go.mod, go.sum, Makefile, Dockerfiles,
   .ci-operator.yaml, AGENTS.md):
   ```
   OCPCLOUD-XXXX: Bump k8s to K8S_MINOR and Go to GO_MINOR
   ```

2. **Vendor update** (vendor/ directory only):
   ```
   OCPCLOUD-XXXX: Update vendored dependencies
   ```

3. **Code changes** (if any, in a separate commit):
   ```
   OCPCLOUD-XXXX: Fix code for k8s K8S_MINOR compatibility
   ```

Do NOT push or create a PR unless the user asks.

## Pre-merge checklist

Before marking the bump as ready, verify every item:

- [ ] `go.mod` — Go version, k8s.io/*, controller-runtime, controller-tools,
  OpenShift deps all updated
- [ ] `go.mod` — k8s.io module patch version matches `ENVTEST_K8S_VERSION`
- [ ] Vendor — `go mod vendor` ran cleanly, vendor directory updated
- [ ] `Makefile` — `ENVTEST_K8S_VERSION` matches envtest-releases.yaml
- [ ] `Makefile` — `BUILD_IMAGE` updated
- [ ] `Dockerfile` — builder image updated
- [ ] `Dockerfile.rhel` — builder and base images updated
- [ ] `.ci-operator.yaml` — build root image tag updated
- [ ] `AGENTS.md` — `setup-envtest use` version references updated
- [ ] CRD paths — `CRDDirectoryPaths` in test suites match vendor layout
- [ ] Code compiles — `make build` passes (use `CGO_ENABLED=0` on macOS)
- [ ] Tests pass — `make test` passes
- [ ] Format clean — `make fmt` produces no changes
- [ ] Lint passes — `make vet` passes

## Troubleshooting

### go mod tidy fails with missing package

If `go mod tidy` fails because `k8s.io/api` does not contain a package (e.g.
`scheduling/v1alpha1`), a transitive dependency still references a removed API
group. Ensure `k8s.io/kubectl`, `k8s.io/cli-runtime`, and all other `k8s.io/*`
modules are at the same `v0.XX.Y` version. The most common cause is
`machine-api-operator` pinning an older `k8s.io/kubectl`.

### controller-runtime build errors

If you see errors like `does not implement ResourceEventHandlerRegistration
(missing method HasSyncedChecker)`, the controller-runtime version does not
match the k8s client-go version. Bump controller-runtime to the version that
targets the new k8s minor (see the version mapping table in Step 1b).

### MAO resolves to old version

`go get github.com/openshift/machine-api-operator@master` resolves to a stale
branch. MAO's default branch is `main`. Always use:

```bash
go get github.com/openshift/machine-api-operator@main
```

### macOS build failure with -static

The Makefile passes `-extldflags -static` which the macOS linker does not
support (`library 'crt0.o' not found`). For local development:

```bash
CGO_ENABLED=0 make build
```

This produces a pure Go binary without requiring the C linker.

### Vendor conflicts

If `go mod vendor` produces unexpected diffs, run `rm -rf vendor && go mod
vendor` to rebuild from scratch.

### ENVTEST_K8S_VERSION assets not available

Envtest asset availability is checked as a prerequisite (see the "Envtest assets"
section above). If assets are missing, the bump should not proceed — tests will
fail in CI. Check with the openshift/api maintainers.
