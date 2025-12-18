# AGENTS.md

This file provides guidance to AI Agents when working with the machine-api-provider-gcp project.

## Project Overview

The Machine API Provider GCP implements the Machine API provider for Google Cloud Platform in OpenShift clusters, enabling declarative management of GCP Compute Engine instances as Kubernetes nodes.

### Architecture

| Binary | Location | Purpose |
|--------|----------|---------|
| machine-controller-manager | `cmd/manager/` | Main controller; reconciles Machine CRs into GCP VMs |
| termination-handler | `cmd/termination-handler/` | Handles spot/preemptible instance preemption |

> **Note:** This is the GCP-specific provider. The main Machine API controller lives in [machine-api-operator](https://github.com/openshift/machine-api-operator).

### Key Packages

| Package | Purpose |
|---------|---------|
| `pkg/cloud/gcp/actuators/machine/` | Machine lifecycle (create/delete/update GCP instances) |
| `pkg/cloud/gcp/actuators/machineset/` | Autoscaler annotations (vCPU, memory, GPU) |
| `pkg/cloud/gcp/actuators/services/compute/` | GCP Compute API wrapper interface |
| `pkg/cloud/gcp/actuators/services/tags/` | GCP Resource Manager Tags API wrapper |
| `pkg/cloud/gcp/actuators/util/` | Credentials, labels, UEFI checks, marshaling |
| `pkg/termination/` | Spot instance termination detection |

### Key Patterns
- Uses `GCPComputeService` interface for all GCP API calls (enables mocking)
- Actuator pattern: `Create()`, `Update()`, `Delete()`, `Exists()` methods
- Machine scope encapsulates request context (credentials, clients, spec/status)
- Vendored dependencies (`go mod vendor`, use `GOFLAGS=-mod=vendor`)
- Feature gates controlled via OpenShift's featuregates mechanism

## Quick Reference

### Essential Commands
```bash
make build              # Build all binaries
make test               # Run all tests (Ginkgo + envtest)
make fmt                # Format code
make vet                # Run go vet
make sec                # Run gosec security scanner
make vendor             # Update vendor directory
```

## Testing

```bash
make test               # All unit tests with envtest
make unit               # Alias for make test
make test-e2e           # E2E tests (requires KUBECONFIG)
```

### Running Specific Package Tests
```bash
KUBEBUILDER_ASSETS="$(go run ./vendor/sigs.k8s.io/controller-runtime/tools/setup-envtest use 1.34.1 -p path --bin-dir ./bin --index https://raw.githubusercontent.com/openshift/api/master/envtest-releases.yaml)" \
go run ./vendor/github.com/onsi/ginkgo/v2/ginkgo -v ./pkg/cloud/gcp/actuators/machine/...
```

### Test Patterns
- Tests use **Ginkgo/Gomega** with **envtest** for K8s API simulation; use **komega** where possible for Kubernetes object assertions
- Mock `GCPComputeService` interface for unit tests
- Each controller has a `*_suite_test.go` for setup
- Follow existing test patterns in `*_test.go` files

### Container Engine
- Defaults to `podman`, falls back to `docker`
- `USE_DOCKER=1` to force Docker
- `NO_DOCKER=1` to run locally without containers

## Do

- Run `make fmt && make vet` before committing
- Run `make test` to verify changes
- Use `GCPComputeService` interface for all GCP API operations
- Add mock implementations when extending `GCPComputeService`
- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use `klog` for logging (never `fmt.Printf` or `log`)
- Use `InvalidMachineConfiguration` for 4xx GCP API errors
- Check `pkg/cloud/gcp/actuators/machine/reconciler.go` for patterns

## Do NOT

- Edit files under `vendor/` directly
- Call GCP APIs directly (always use `GCPComputeService` interface)
- Return naked errors without context
- Hardcode project IDs, zones, or machine types
- Log credentials, service account keys, or OAuth tokens
- Forget to run `go mod vendor` after changing dependencies
- Add the `go mod vendor` result in a commit with the implementation changes
- Skip UEFI compatibility checks when modifying disk-related code

## Project Structure

```
machine-api-provider-gcp/
├── cmd/
│   ├── manager/                    ⭐ Main controller entry point
│   │   └── main.go                 # Manager setup, actuator init
│   └── termination-handler/        ⭐ Spot instance termination
│       └── main.go                 # Preemption detection
│
├── pkg/
│   ├── cloud/gcp/actuators/
│   │   ├── machine/                ⭐ Core machine reconciliation
│   │   │   ├── actuator.go         # CRUD interface implementation
│   │   │   ├── reconciler.go       # Instance create/update/delete logic
│   │   │   ├── machine_scope.go    # Request-scoped context
│   │   │   └── conditions.go       # Status condition handling
│   │   │
│   │   ├── machineset/             ⭐ MachineSet controller
│   │   │   ├── controller.go       # Autoscaler annotations
│   │   │   └── cache.go            # Machine type caching
│   │   │
│   │   ├── services/               ⭐ GCP API wrappers
│   │   │   ├── compute/
│   │   │   │   ├── computeservice.go      # Interface + implementation
│   │   │   │   └── computeservice_mock.go # Test mocks
│   │   │   └── tags/
│   │   │       ├── tagservice.go          # Resource Manager tags
│   │   │       └── tagservice_mock.go     # Test mocks
│   │   │
│   │   └── util/                   ⭐ Shared utilities
│   │       ├── gcp_credentials.go         # Secret retrieval
│   │       ├── gcp_machine_architecture.go # CPU arch detection
│   │       ├── gcp_tags_labels.go         # Label/tag processing
│   │       ├── gcp_uefi_disk_check.go     # UEFI compatibility
│   │       └── register.go                # Spec/status marshaling
│   │
│   ├── termination/                ⭐ Termination handler logic
│   │   └── termination.go          # Metadata polling, node marking
│   │
│   └── version/
│       └── version.go              # Build version info
│
├── config/                         # Kubernetes manifests
├── hack/                           # Build and test scripts
├── Makefile                        # Build targets
└── go.mod                          # Dependencies
```


## Troubleshooting

**Linter failures:**
```bash
make fmt    # Fix formatting
make vet    # Check for issues
```

**Test failures - check envtest setup:**
```bash
KUBEBUILDER_ASSETS="$(go run ./vendor/sigs.k8s.io/controller-runtime/tools/setup-envtest use 1.34.1 -p path --bin-dir ./bin --index https://raw.githubusercontent.com/openshift/api/master/envtest-releases.yaml)" make test
```
**GCP API Error Codes:**
- `400`: Invalid configuration (zone, machine type, etc.)
- `403`: Permission denied (check IAM)
- `404`: Resource not found (image, network, etc.)
- `409`: Already exists (name conflict)
- `429`: Quota exceeded

## Related Repositories

- [openshift/api](https://github.com/openshift/api) - `GCPMachineProviderSpec` definition
- [openshift/machine-api-operator](https://github.com/openshift/machine-api-operator) - Deploys this provider
- [openshift/cluster-api-actuator-pkg](https://github.com/openshift/cluster-api-actuator-pkg) - E2E testing framework

## Dependency Flow
openshift/api (GCPMachineProviderSpec)
  → machine-api-provider-gcp (this repo, implements actuator)
  → machine-api-operator (deploys this provider)

## Code Examples

### ✅ GOOD: Using GCPComputeService Interface

```go
// Always use the interface for GCP operations
type Reconciler struct {
    computeService computeservice.GCPComputeService
}

func (r *Reconciler) createInstance() error {
    instance := &compute.Instance{
        Name:        r.machine.Name,
        MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", zone, r.providerSpec.MachineType),
    }
    _, err := r.computeService.InstancesInsert(r.projectID, zone, instance)
    if err != nil {
        return fmt.Errorf("failed to create instance: %w", err)
    }
    return nil
}
```

### ✅ GOOD: Error Handling with InvalidMachineConfiguration

```go
func (r *Reconciler) create() error {
    if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
        return machinecontroller.InvalidMachineConfiguration("failed validating machine provider spec: %v", err)
    }

    _, err := r.computeService.InstancesInsert(r.projectID, zone, instance)
    if err != nil {
        if googleError, ok := err.(*googleapi.Error); ok {
            // 4xx errors indicate client misconfiguration
            if strings.HasPrefix(strconv.Itoa(googleError.Code), "4") {
                return machinecontroller.InvalidMachineConfiguration("error launching instance: %v", googleError.Error())
            }
        }
        return fmt.Errorf("failed to create instance via compute service: %w", err)
    }
    return nil
}
```

### ✅ GOOD: Testing with Mocks

```go
func TestCreate(t *testing.T) {
    mockComputeService := &computeservice.MockGCPComputeService{
        InstancesInsertFunc: func(project, zone string, instance *compute.Instance) (*compute.Operation, error) {
            return &compute.Operation{Status: "DONE"}, nil
        },
    }

    actuator := machine.NewActuator(machine.ActuatorParams{
        ComputeClientBuilder: func(string) (computeservice.GCPComputeService, error) {
            return mockComputeService, nil
        },
    })
    // ... test assertions
}
```

### ❌ BAD: Direct GCP API Calls

```go
// DON'T DO THIS - bypasses interface for testing
service, _ := compute.NewService(ctx)
service.Instances.Insert(project, zone, instance).Do()

// USE THE INTERFACE (see above)
```

### ❌ BAD: Naked Errors

```go
// DON'T DO THIS
if err := r.computeService.InstancesInsert(...); err != nil {
    return err
}

// WRAP ERRORS WITH CONTEXT
if err := r.computeService.InstancesInsert(...); err != nil {
    return fmt.Errorf("failed to create instance %s: %w", name, err)
}
```
