# Vendored API Changes from openshift/api#2795

## Summary

Successfully vendored the dual stack networking API changes from https://github.com/openshift/api/pull/2795 into the `dual-stack-support` branch.

## Changes Vendored

### API Types (from barbacbd/api@7f681bb588e9)

**New Types:**
- `StackType`: Enum with values `IPv4Only` | `DualStack`
- `IPv6AccessType`: Enum with values `External` | `Internal`

**New Fields on `GCPNetworkInterface`:**
- `stackType`: Determines IP stack configuration (defaults to `IPv4Only`)
- `ipv6Address`: Static IPv6 internal address (optional)
- `ipv6AccessType`: IPv6 access type - External or Internal (defaults to `External`)

## Implementation Changes

Updated reconciler to work with the vendored API:

### reconciler.go
1. **Stack Type Mapping**: Maps API `StackType` to GCP Compute API values:
   - `IPv4Only` → `IPV4_ONLY`
   - `DualStack` → `IPV4_IPV6`

2. **IPv6 Access Configuration**: Creates `Ipv6AccessConfigs` when:
   - `stackType` is `DualStack` AND
   - `ipv6AccessType` is `External`

3. **Static IPv6 Address**: Sets `Ipv6Address` on network interface when provided

4. **Validation**: Ensures:
   - `stackType` is either `IPv4Only` or `DualStack`
   - `ipv6AccessType` is only set for `DualStack`
   - `ipv6Address` is only set for `DualStack`

### reconciler_test.go
Updated all test cases to use the vendored API types:
- ✅ Dual stack with external IPv6 access
- ✅ Dual stack with internal IPv6 only  
- ✅ Dual stack with static IPv6 address
- ✅ Default to IPv4 only (backward compatible)
- ✅ Validation error cases

## Test Results

All dual stack tests pass:
```
--- PASS: TestCreate/Dual_stack_networking_with_external_IPv6_access (0.00s)
--- PASS: TestCreate/Dual_stack_networking_with_internal_IPv6_only (0.00s)
--- PASS: TestCreate/Dual_stack_with_static_IPv6_address (0.00s)
--- PASS: TestCreate/Default_to_IPv4_only_when_stackType_not_specified (0.00s)
--- PASS: TestCreate/Invalid_stackType_produces_error (0.00s)
--- PASS: TestCreate/IPv6AccessType_with_IPv4_only_produces_error (0.00s)
--- PASS: TestCreate/Invalid_IPv6AccessType_produces_error (0.00s)
--- PASS: TestCreate/IPv6Address_with_IPv4_only_produces_error (0.00s)
--- PASS: TestReconcileMachineWithCloudStateDualStack (0.00s)
```

## go.mod Changes

Added replace directive to vendor from PR:
```go
replace github.com/openshift/api => github.com/barbacbd/api v0.0.0-20260406135515-7f681bb588e9
```

## Configuration Example

```yaml
apiVersion: machine.openshift.io/v1beta1
kind: Machine
metadata:
  name: worker-dual-stack
spec:
  providerSpec:
    value:
      networkInterfaces:
      - network: my-network
        subnetwork: my-subnet
        publicIP: true
        stackType: DualStack        # IPv4 + IPv6
        ipv6AccessType: External     # External IPv6 access
        ipv6Address: 2600:1900:4000:318::  # Optional static IPv6
```

## Next Steps

This branch is now ready to create worker machines with dual stack networking once:
1. The upstream PR (openshift/api#2795) is merged
2. The replace directive is removed
3. Dependencies are updated to use the merged version

## Files Modified

- `go.mod` - Added replace directive
- `go.sum` - Updated checksums
- `vendor/github.com/openshift/api/machine/v1beta1/types_gcpprovider.go` - Vendored API types
- `pkg/cloud/gcp/actuators/machine/reconciler.go` - Reconciler implementation
- `pkg/cloud/gcp/actuators/machine/reconciler_test.go` - Test cases
- `pkg/cloud/gcp/actuators/services/compute/computeservice_mock.go` - Mock helper
