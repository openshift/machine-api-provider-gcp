package util

import (
	"fmt"

	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
)

// EndpointLookupFuncType is function type for finding overridden gcp service endpoints
type EndpointLookupFuncType func(client controllerclient.Client, endpointName configv1.GCPServiceEndpointName) (*configv1.GCPServiceEndpoint, error)

// GetGCPServiceEndpoint finds the GCP Service Endpoint override information for a provided service.
func GetGCPServiceEndpoint(client controllerclient.Client, endpointName configv1.GCPServiceEndpointName) (*configv1.GCPServiceEndpoint, error) {
	infra, err := GetInfrastructure(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster infrastructure: %w", err)
	}

	if infra != nil && infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.GCP != nil {
		for _, endpoint := range infra.Status.PlatformStatus.GCP.ServiceEndpoints {
			if endpoint.Name == endpointName {
				return &endpoint, nil
			}
		}
	}
	return nil, nil
}
