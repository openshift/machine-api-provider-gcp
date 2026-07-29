# OpenShift cluster-api-provider-gcp

This repository hosts an implementation of a provider for GCP for the
OpenShift [machine-api](https://github.com/openshift/cluster-api).

This provider runs as a machine-controller deployed by the
[machine-api-operator](https://github.com/openshift/machine-api-operator)

For troubleshooting Makefile permission issues see [hacking-guide](https://github.com/openshift/machine-api-operator/blob/master/docs/dev/hacking-guide.md#troubleshooting-make-targets).

## Development

- [Bumping Kubernetes and Go](docs/development/bump-k8s-go.md)

## TargetPools
Target pools exist in a *region*

Regions have multiple *zones*

Instances associated with Target Pools must be in the same *region* as
the target pool.
