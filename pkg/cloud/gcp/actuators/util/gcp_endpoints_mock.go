package util

import (
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
)

// GetGCPServiceEndpoint finds the GCP Service Endpoint override information for a provided service.
func MockGCPEndpointLookup(client controllerclient.Client, endpointName configv1.GCPServiceEndpointName) (*configv1.GCPServiceEndpoint, error) {
	return nil, nil
}
