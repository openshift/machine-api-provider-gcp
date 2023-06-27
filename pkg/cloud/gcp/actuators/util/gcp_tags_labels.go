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
	"time"

	rscmgr "cloud.google.com/go/resourcemanager/apiv3"
	rscmgrpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"golang.org/x/time/rate"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"

	"k8s.io/klog/v2"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// globalInfrastructureName is the default name of the Infrastructure object
	globalInfrastructureName = "cluster"

	// resourceManagerEndPoint is the host name to be used for binding tag values
	// to the VM resources.
	resourceManagerEndPoint = "cloudresourcemanager.googleapis.com"

	// computeAPIEndPoint is the endpoint for identifying the VM resource
	computeAPIEndPoint = "//compute.googleapis.com"
)

type GCPTags struct {
	CoreClient   controllerclient.Client
	Namespace    string
	ProviderSpec machinev1.GCPMachineProviderSpec
	ProjectID    string
	InstanceID   uint64
	InstanceZone string
	TagValues    []string
}

func GetInfrastructure(client controllerclient.Client) (*configv1.Infrastructure, error) {
	infra := &configv1.Infrastructure{}
	infraName := controllerclient.ObjectKey{Name: globalInfrastructureName}

	if err := client.Get(context.Background(), infraName, infra); err != nil {
		return nil, fmt.Errorf("failed to get infrastructure: %w", err)
	}

	return infra, nil
}

func newLimiter(limit, burst int, emptyBucket bool) *rate.Limiter {
	limiter := rate.NewLimiter(rate.Every(time.Second/time.Duration(limit)), burst)

	if emptyBucket {
		// Start with empty bucket to exert control
		// over token renewal rate during the first burst.
		limiter.AllowN(time.Now(), burst)
	}

	return limiter
}

func getInfraResourceTagsList(platformStatus *configv1.PlatformStatus) (tags map[string]string) {
	if platformStatus != nil && platformStatus.GCP != nil && platformStatus.GCP.ResourceTags != nil {
		tags = make(map[string]string, len(platformStatus.GCP.ResourceTags))
		for _, tag := range platformStatus.GCP.ResourceTags {
			tags[tag.Key] = tag.Value
		}
	}
	return
}

func GetTagsList(platformStatus *configv1.PlatformStatus, providerSpec *machinev1.GCPMachineProviderSpec) ([]string, error) {
	tags := getInfraResourceTagsList(platformStatus)

	if len(tags) < 0 && len(providerSpec.UserTags) < 0 {
		return nil, nil
	}

	if platformStatus.GCP.OrganizationID == "" &&
		providerSpec.OrganizationID == "" {
		return nil, fmt.Errorf("organizationID must be defined either in" +
			"infrstructure.status or Machine.Spec.ProviderSpec")
	}
	orgID := getOrganizationID(platformStatus.GCP.OrganizationID, providerSpec.OrganizationID)

	if tags == nil {
		return toTagValueList(orgID, providerSpec.UserTags), nil
	}

	// merge tags present in Infrastructure.Status with
	// the tags configured in GCPMachineProviderSpec, with
	// precedence given to those in GCPMachineProviderSpec
	// for new or updated tags.
	for k, v := range providerSpec.UserTags {
		tags[k] = v
	}

	if len(tags) > 50 {
		return nil, fmt.Errorf("maximum of 50 tags can be added to a VM instance, "+
			"infrstructure.status.resourceTags Machine.Spec.ProviderSpec.UserTags put together configured tag count is %d", len(tags))
	}

	return toTagValueList(orgID, tags), nil
}

func (tags *GCPTags) ApplyTagsToComputeInstance(ctx context.Context) error {
	if len(tags.TagValues) < 0 {
		return nil
	}

	parent := fmt.Sprintf("%s/projects/%s/zones/%s/instances/%d",
		computeAPIEndPoint, tags.ProjectID, tags.InstanceZone, tags.InstanceID)

	client, err := tags.getTagBindingsClient(ctx)
	if err != nil || client == nil {
		return fmt.Errorf("failed to create tag binding client for adding tags to %d machine: %w", tags.InstanceID, err)
	}
	defer client.Close()

	filteredTags := getFilteredTagList(ctx, client, parent, tags.TagValues)

	// GCP has a rate limit of 10 requests per second. we will
	// restrict to 8.
	limiter := newLimiter(8, 8, true)

	tagBindingReq := &rscmgrpb.CreateTagBindingRequest{
		TagBinding: &rscmgrpb.TagBinding{
			Parent: parent,
		},
	}
	errFlag := false
	for _, value := range filteredTags {
		if err := limiter.Wait(ctx); err != nil {
			errFlag = true
			klog.Errorf("rate limiting request to add %s tag to %d VM failed: %w", value, tags.InstanceID, err)
		}

		tagBindingReq.TagBinding.TagValueNamespacedName = value
		result, err := client.CreateTagBinding(ctx, tagBindingReq)
		if err != nil {
			errFlag = true
			klog.Errorf("request to add %s tag to %d VM failed", value, tags.InstanceID)
		}

		if _, err = result.Wait(ctx); err != nil {
			errFlag = true
			klog.Errorf("failed to add %s tag to %d VM", value, tags.InstanceID)
		}
	}
	if errFlag {
		return fmt.Errorf("failed to add tags to %d VM", tags.InstanceID)
	}
	return nil
}

func (tags *GCPTags) getTagBindingsClient(ctx context.Context) (*rscmgr.TagBindingsClient, error) {
	cred, err := GetCredentialsSecret(tags.CoreClient, tags.Namespace, tags.ProviderSpec)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("https://%s-%s", tags.InstanceZone, resourceManagerEndPoint)
	opts := []option.ClientOption{
		option.WithCredentialsJSON([]byte(cred)),
		option.WithEndpoint(endpoint),
	}
	return rscmgr.NewTagBindingsRESTClient(ctx, opts...)
}

func getFilteredTagList(ctx context.Context, client *rscmgr.TagBindingsClient, parent string, tagList []string) []string {
	dupTags := make(map[string]bool, len(tagList))
	for _, k := range tagList {
		dupTags[k] = false
	}

	listBindingsReq := &rscmgrpb.ListEffectiveTagsRequest{
		Parent: parent,
	}
	bindings := client.ListEffectiveTags(ctx, listBindingsReq)
	for {
		binding, read := bindings.Next()
		if read == iterator.Done {
			break
		}
		if _, exist := dupTags[binding.GetNamespacedTagValue()]; exist {
			dupTags[binding.GetNamespacedTagValue()] = true
		}
	}

	filteredTags := make([]string, 0, len(tagList))
	for tagValue, dup := range dupTags {
		if !dup {
			filteredTags = append(filteredTags, tagValue)
		}
	}

	return filteredTags
}

func getOrganizationID(source1, source2 string) string {
	if source1 != "" {
		return source1
	}
	return source2
}

func toTagValueList(orgID string, tags map[string]string) []string {
	if len(tags) < 0 {
		return nil
	}

	list := make([]string, 0, len(tags))
	for k, v := range tags {
		tag := fmt.Sprintf("%s/%s/%s", orgID, k, v)
		list = append(list, tag)
	}
	return list
}
