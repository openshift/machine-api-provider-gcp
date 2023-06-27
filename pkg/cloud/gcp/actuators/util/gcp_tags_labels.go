/*
Copyright The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	globalInfrastructureName = "cluster"

	// ocpDefaultLabelFmt is the format string for the default label
	// added to the OpenShift created GCP resources.
	ocpDefaultLabelFmt = "kubernetes-io-cluster-%s"
)

func GetInfrastructure(client controllerclient.Client) (*configv1.Infrastructure, error) {
	infra := &configv1.Infrastructure{}
	infraName := controllerclient.ObjectKey{Name: globalInfrastructureName}

	if err := client.Get(context.Background(), infraName, infra); err != nil {
		return nil, fmt.Errorf("failed to get infrastructure: %w", err)
	}

	return infra, nil
}

func getInfraResourceLabels(platformStatus *configv1.PlatformStatus) (labels map[string]string, err error) {
	if platformStatus != nil && platformStatus.GCP != nil && platformStatus.GCP.ResourceLabels != nil {
		labels = make(map[string]string, len(platformStatus.GCP.ResourceLabels))
		for _, tag := range platformStatus.GCP.ResourceLabels {
			labels[tag.Key] = tag.Value
		}
	}
	return
}

func getOCPLabels(clusterID string) (map[string]string, error) {
	if clusterID == "" {
		return nil, fmt.Errorf("cluster ID required for generating OCP tag list")
	}
	return map[string]string{
		fmt.Sprintf(ocpDefaultLabelFmt, clusterID): "owned",
	}, nil
}

func GetLabelsList(clusterID string, platform *configv1.PlatformStatus, providerSpecLabels map[string]string) (map[string]string, error) {
	ocpLabels, err := getOCPLabels(clusterID)
	if err != nil {
		return nil, err
	}

	infraLabels, err := getInfraResourceLabels(platform)
	if err != nil {
		return nil, err
	}

	if infraLabels == nil && providerSpecLabels == nil {
		return ocpLabels, nil
	}

	labels := make(map[string]string)
	// copy user defined labels in platform.Infrastructure.Status.
	for k, v := range infraLabels {
		labels[k] = v
	}

	// merge labels present in Infrastructure.Status with
	// the labels configured in GCPMachineProviderSpec, with
	// precedence given to those in GCPMachineProviderSpec
	// for new or updated labels.
	for k, v := range providerSpecLabels {
		labels[k] = v
	}

	// copy OCP labels, overwrite any OCP reserved labels found in
	// the user defined label list.
	for k, v := range ocpLabels {
		labels[k] = v
	}

	if len(ocpLabels) > 32 || (len(labels)-len(ocpLabels)) > 32 {
		return nil, fmt.Errorf("ocp can define upto 32 labels and user can define upto 32 labels,"+
			"infrstructure.status.resourceLabels and Machine.Spec.ProviderSpec.Labels put together configured label count is %d", len(labels))
	}

	return labels, nil
}
