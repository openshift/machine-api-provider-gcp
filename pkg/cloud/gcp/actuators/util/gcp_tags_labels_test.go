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
	"net/http"
	"reflect"
	"testing"

	tagservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/tags"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"

	"github.com/googleapis/gax-go/v2/apierror"
	tags "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/googleapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
	controllerfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testResourceManagerTag is for holding the tags to be used in tests.
type testResourceManagerTag struct {
	ParentID string
	Key      string
	Value    string
}

// testTagKV is for holding the tags' key and value to be used in tests.
type testTagKV struct {
	key   string
	value string
}

func TestGetLabelsList(t *testing.T) {
	machineClusterID := "test-3546b"

	infraObj := configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastructureName,
		},
		Spec: configv1.InfrastructureSpec{
			PlatformSpec: configv1.PlatformSpec{
				Type: configv1.GCPPlatformType,
			},
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: machineClusterID,
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.GCPPlatformType,
				GCP:  &configv1.GCPPlatformStatus{},
			},
		},
	}

	testCases := []struct {
		name              string
		getInfra          func() *configv1.Infrastructure
		machineSpecLabels map[string]string
		expectedLabels    map[string]string
		wantErr           bool
	}{
		{
			name:              "Infrastructure resource doesn't exist",
			getInfra:          func() *configv1.Infrastructure { return nil },
			machineSpecLabels: nil,
			expectedLabels:    nil,
			wantErr:           true,
		},
		{
			name: "user-defined labels is empty",
			getInfra: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraObj.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{}
				return infra
			},
			machineSpecLabels: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			expectedLabels: map[string]string{
				"key1": "value1", "key2": "value2",
				fmt.Sprintf("kubernetes-io-cluster-%s", machineClusterID): "owned",
			},
			wantErr: false,
		},
		{
			name: "user-defined labels success scenario",
			getInfra: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraObj.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{
					{Key: "key1", Value: "value1"}, {Key: "key2", Value: "value2"},
				}
				return infra
			},
			machineSpecLabels: map[string]string{
				"key3": "value3",
				"key4": "value4",
			},
			expectedLabels: map[string]string{
				"key1": "value1", "key2": "value2", "key3": "value3", "key4": "value4",
				fmt.Sprintf("kubernetes-io-cluster-%s", machineClusterID): "owned",
			},
			wantErr: false,
		},
		{
			name: "providerSpec labels is empty",
			getInfra: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraObj.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{
					{Key: "key1", Value: "value1"}, {Key: "key2", Value: "value2"},
				}
				return infra
			},
			machineSpecLabels: nil,
			expectedLabels: map[string]string{
				"key1": "value1", "key2": "value2",
				fmt.Sprintf("kubernetes-io-cluster-%s", machineClusterID): "owned",
			},
			wantErr: false,
		},
		{
			name: "providerSpec labels maxes allowed limit",
			getInfra: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraObj.DeepCopyInto(infra)
				return infra
			},
			machineSpecLabels: map[string]string{
				"key1": "value40", "key2": "value39", "key3": "value38",
				"key4": "value37", "key5": "value36", "key6": "value35",
				"key7": "value34", "key8": "value33", "key9": "value32",
				"key10": "value31", "key11": "value30", "key12": "value29",
				"key13": "value28", "key14": "value27", "key15": "value26",
				"key16": "value25", "key17": "value24", "key18": "value23",
				"key19": "value22", "key20": "value21", "key21": "value20",
				"key22": "value19", "key23": "value18", "key24": "value17",
				"key25": "value16", "key26": "value15", "key27": "value14",
				"key28": "value13", "key29": "value12", "key30": "value11",
				"key31": "value10", "key32": "value9", "key33": "value8",
				"key34": "value7", "key35": "value6", "key36": "value5",
				"key37": "value4", "key38": "value3", "key39": "value2",
				"key40": "value1",
			},
			expectedLabels: nil,
			wantErr:        true,
		},
		{
			name: "user-defined labels maxes allowed limit",
			getInfra: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraObj.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{
					{Key: "key1", Value: "value40"}, {Key: "key2", Value: "value39"}, {Key: "key3", Value: "value38"},
					{Key: "key4", Value: "value37"}, {Key: "key5", Value: "value36"}, {Key: "key6", Value: "value35"},
					{Key: "key7", Value: "value34"}, {Key: "key8", Value: "value33"}, {Key: "key9", Value: "value32"},
					{Key: "key10", Value: "value31"}, {Key: "key11", Value: "value30"}, {Key: "key12", Value: "value29"},
					{Key: "key13", Value: "value28"}, {Key: "key14", Value: "value27"}, {Key: "key15", Value: "value26"},
					{Key: "key16", Value: "value25"}, {Key: "key17", Value: "value24"}, {Key: "key18", Value: "value23"},
					{Key: "key19", Value: "value22"}, {Key: "key20", Value: "value21"}, {Key: "key21", Value: "value20"},
					{Key: "key22", Value: "value19"}, {Key: "key23", Value: "value18"}, {Key: "key24", Value: "value17"},
					{Key: "key25", Value: "value16"}, {Key: "key26", Value: "value15"}, {Key: "key27", Value: "value14"},
					{Key: "key28", Value: "value13"}, {Key: "key29", Value: "value12"}, {Key: "key30", Value: "value11"},
					{Key: "key31", Value: "value10"}, {Key: "key32", Value: "value9"}, {Key: "key33", Value: "value8"},
					{Key: "key34", Value: "value7"}, {Key: "key35", Value: "value6"}, {Key: "key36", Value: "value5"},
					{Key: "key37", Value: "value4"}, {Key: "key38", Value: "value3"}, {Key: "key39", Value: "value2"},
				}
				return infra
			},
			machineSpecLabels: map[string]string{
				"key51": "value51",
				"key52": "value52",
			},
			expectedLabels: nil,
			wantErr:        true,
		},
		{
			name: "providerSpec and user-defined labels have duplicates",
			getInfra: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraObj.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{
					{Key: "key1", Value: "value1"}, {Key: "key2", Value: "value2"}, {Key: "key4", Value: "value4"},
				}
				return infra
			},
			machineSpecLabels: map[string]string{
				"key1": "value11",
				"key2": "value2",
				"key3": "key3",
			},
			expectedLabels: map[string]string{
				"key1": "value11", "key2": "value2", "key3": "key3", "key4": "value4",
				fmt.Sprintf("kubernetes-io-cluster-%s", machineClusterID): "owned",
			},
			wantErr: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			infra := tc.getInfra()
			scheme := runtime.NewScheme()
			if err := configv1.Install(scheme); err != nil {
				t.Errorf("failed to add scheme to fake client")
			}

			var fakeClient controllerclient.Client
			if infra != nil {
				fakeClient = controllerfake.NewClientBuilder().WithScheme(scheme).WithObjects(infra).Build()
			} else {
				fakeClient = controllerfake.NewClientBuilder().WithScheme(scheme).Build()
			}

			labels, err := GetLabelsList(fakeClient, machineClusterID, tc.machineSpecLabels)

			if (err != nil) != tc.wantErr {
				t.Errorf("Got: %v, wantErr: %v", err, tc.wantErr)
			}

			if !reflect.DeepEqual(labels, tc.expectedLabels) {
				t.Errorf("Expected %+v, Got: %+v, Infrastructure: %+v",
					tc.expectedLabels, labels, infra)
			}
		})
	}
}

// generateTestTagSet generates tags for tests which can be converted to
// machinev1.ResourceManagerTag and configv1.GCPResourceTag types.
func generateTestTagSet() []testResourceManagerTag {
	tagset := make([]testResourceManagerTag, 0, 55)

	for i := 1; i <= 52; i++ {
		tagset = append(tagset, testResourceManagerTag{
			ParentID: "openshift",
			Key:      fmt.Sprintf("key%d", i),
			Value:    fmt.Sprintf("value%d", i),
		})
	}
	tagset = append(tagset, []testResourceManagerTag{
		{ParentID: "openshift",
			Key:   "key101",
			Value: "value101"},
		{ParentID: "openshift",
			Key:   "key201",
			Value: "value201"},
		{ParentID: "openshift",
			Key:   "key501",
			Value: "value501"},
	}...)

	return tagset
}

// generateTestTagsKeyValueNameMap generates map containing the tag
// Key(`tagKeys/{tag_key_id}`) and Value(`tagValues/{tag_value_id}`) Names
// mapped with the tag's NamespacedName (`{parentId}/{tagKeyShort}/{tagValueShort}`).
func generateTestTagsKeyValueNameMap() map[string]testTagKV {
	tagKVMap := make(map[string]testTagKV, 55)

	for i := 1; i <= 52; i++ {
		tagKVMap[fmt.Sprintf("openshift/key%d/value%d", i, i)] = testTagKV{
			key:   fmt.Sprintf("tagKeys/%d", i),
			value: fmt.Sprintf("tagValues/%d", i),
		}
	}
	tagKVMap["openshift/key1/value101"] = testTagKV{
		key:   "tagKeys/1",
		value: "tagValues/101"}
	tagKVMap["openshift/key2/value201"] = testTagKV{
		key:   "tagKeys/2",
		value: "tagValues/201"}
	tagKVMap["openshift/key5/value501"] = testTagKV{
		key:   "tagKeys/5",
		value: "tagValues/501"}

	return tagKVMap
}

func TestGetResourceManagerTags(t *testing.T) {
	var (
		ctx      = context.Background()
		infraRef = &configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: globalInfrastructureName,
			},
			Spec: configv1.InfrastructureSpec{
				PlatformSpec: configv1.PlatformSpec{
					Type: configv1.GCPPlatformType,
				},
			},
			Status: configv1.InfrastructureStatus{
				InfrastructureName: "test-3546b",
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.GCPPlatformType,
					GCP:  &configv1.GCPPlatformStatus{},
				},
			},
		}

		testTagSets             = generateTestTagSet()
		testTagsKeyValueNameMap = generateTestTagsKeyValueNameMap()
		getInfraObj             = func(infraRef *configv1.Infrastructure, tags []testResourceManagerTag) *configv1.Infrastructure {
			if infraRef == nil {
				return nil
			}
			infraObj := new(configv1.Infrastructure)
			infraRef.DeepCopyInto(infraObj)
			if len(tags) > 0 {
				infraTags := make([]configv1.GCPResourceTag, 0, len(tags))
				for _, tag := range tags {
					infraTags = append(infraTags, configv1.GCPResourceTag{
						ParentID: tag.ParentID,
						Key:      tag.Key,
						Value:    tag.Value,
					})
				}
				infraObj.Status.PlatformStatus.GCP.ResourceTags = infraTags
			}
			return infraObj
		}
		getMachineSpecTags = func(tags []testResourceManagerTag) []machinev1.ResourceManagerTag {
			if len(tags) == 0 {
				return nil
			}
			machineSpecTags := make([]machinev1.ResourceManagerTag, 0, len(tags))
			for _, tag := range tags {
				machineSpecTags = append(machineSpecTags, machinev1.ResourceManagerTag{
					ParentID: tag.ParentID,
					Key:      tag.Key,
					Value:    tag.Value,
				})
			}
			return machineSpecTags
		}
		getExpectedTags = func(tags []string) map[string]string {
			expectedTags := make(map[string]string)
			for _, tag := range tags {
				tagKV := testTagsKeyValueNameMap[tag]
				expectedTags[tagKV.key] = tagKV.value
			}
			return expectedTags
		}
	)

	testCases := []struct {
		name               string
		getInfraObj        func() *configv1.Infrastructure
		getMachineSpecTags func() []machinev1.ResourceManagerTag
		getExpectedTags    func() map[string]string
		wantErr            bool
	}{
		{
			name: "Infrastructure resource doesn't exist",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(nil, nil)
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return nil
			},
			getExpectedTags: func() map[string]string {
				return nil
			},
			wantErr: true,
		},
		{
			name: "user-defined and providerSpec tags is empty",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, nil)
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return nil
			},
			getExpectedTags: func() map[string]string {
				return nil
			},
		},
		{
			name: "user-defined tags is empty",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, nil)
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return getMachineSpecTags(testTagSets[:2])
			},
			getExpectedTags: func() map[string]string {
				return getExpectedTags([]string{
					"openshift/key1/value1",
					"openshift/key2/value2",
				})
			},
		},
		{
			name: "user-defined and providerSpec tags are defined",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, testTagSets[:2])
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return getMachineSpecTags(testTagSets[2:4])
			},
			getExpectedTags: func() map[string]string {
				return getExpectedTags([]string{
					"openshift/key1/value1",
					"openshift/key2/value2",
					"openshift/key3/value3",
					"openshift/key4/value4",
				})
			},
		},
		{
			name: "providerSpec tags is empty",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, testTagSets[:2])
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return nil
			},
			getExpectedTags: func() map[string]string {
				return getExpectedTags([]string{
					"openshift/key1/value1",
					"openshift/key2/value2",
				})
			},
		},
		{
			name: "providerSpec tags maxes allowed limit",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, nil)
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				machineSpecTags := getMachineSpecTags(testTagSets[5:52])
				for i := 0; i <= 3; i++ {
					machineSpecTags = append(machineSpecTags, machinev1.ResourceManagerTag{
						ParentID: testTagSets[i].ParentID,
						Key:      testTagSets[i].Key,
						Value:    testTagSets[i].Value,
					})
				}
				machineSpecTags = append(machineSpecTags, machinev1.ResourceManagerTag{
					ParentID: "openshift",
					Key:      "key5",
					Value:    "value501"})
				return machineSpecTags
			},
			getExpectedTags: func() map[string]string {
				return nil
			},
			wantErr: true,
		},
		{
			name: "user-defined tags maxes allowed limit",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, testTagSets[:52])
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return getMachineSpecTags(testTagSets[52:54])
			},
			getExpectedTags: func() map[string]string {
				return nil
			},
			wantErr: true,
		},
		{
			name: "providerSpec and user-defined tags have duplicates",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, testTagSets[:2])
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				machineSpecTags := getMachineSpecTags(testTagSets[2:3])
				machineSpecTags = append(machineSpecTags, machinev1.ResourceManagerTag{
					ParentID: "openshift",
					Key:      "key1",
					Value:    "value101"})
				return machineSpecTags
			},
			getExpectedTags: func() map[string]string {
				return getExpectedTags([]string{
					"openshift/key1/value101",
					"openshift/key2/value2",
					"openshift/key3/value3",
				})
			},
		},
		{
			name: "providerSpec has non-existent tag(openshift/key5/value5)",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, testTagSets[:2])
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return getMachineSpecTags(testTagSets[1:6])
			},
			getExpectedTags: func() map[string]string {
				return nil
			},
			wantErr: true,
		},
		{
			name: "fetching tag fails with error",
			getInfraObj: func() *configv1.Infrastructure {
				return getInfraObj(infraRef, testTagSets[:2])
			},
			getMachineSpecTags: func() []machinev1.ResourceManagerTag {
				return []machinev1.ResourceManagerTag{
					{
						ParentID: "openshift",
						Key:      "key5",
						Value:    "value500",
					},
				}
			},
			getExpectedTags: func() map[string]string {
				return nil
			},
			wantErr: true,
		},
	}

	tagService := tagservice.NewMockTagService()
	tagService.MockGetNamespacedName = func(ctx context.Context, name string) (*tags.TagValue, error) {
		switch name {
		case "openshift/key5/value5":
			apiErr, _ := apierror.FromError(fmt.Errorf("%w", &googleapi.Error{
				Code:    http.StatusForbidden,
				Message: "Permission denied on resource 'openshift/key5/value5' (or it may not exist).",
			}))
			return nil, apiErr
		case "openshift/key5/value500":
			apiErr, _ := apierror.FromError(fmt.Errorf("%w", &googleapi.Error{
				Code:    http.StatusInternalServerError,
				Message: "Internal error while fetching 'openshift/key5/value500'",
			}))
			return nil, apiErr
		}

		tag, ok := testTagsKeyValueNameMap[name]
		if !ok {
			apiErr, _ := apierror.FromError(fmt.Errorf("%w", &googleapi.Error{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("%s tag does not exist", name),
			}))
			return nil, apiErr
		}

		return &tags.TagValue{
			Name:           tag.value,
			Parent:         tag.key,
			NamespacedName: name,
		}, nil
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			infra := tc.getInfraObj()
			machineSpecTags := tc.getMachineSpecTags()
			expectedTags := tc.getExpectedTags()
			var infraTags []configv1.GCPResourceTag
			if infra != nil && infra.Status.PlatformStatus != nil &&
				infra.Status.PlatformStatus.GCP != nil {
				infraTags = infra.Status.PlatformStatus.GCP.ResourceTags
			}

			scheme := runtime.NewScheme()
			if err := configv1.Install(scheme); err != nil {
				t.Errorf("failed to add scheme to fake client")
			}

			clientBuilder := controllerfake.NewClientBuilder()
			if infra != nil {
				clientBuilder.WithObjects(infra)
			}
			fakeClient := clientBuilder.WithScheme(scheme).Build()

			mergedTags, err := GetResourceManagerTags(ctx, fakeClient, tagService, machineSpecTags)
			if (err != nil) != tc.wantErr {
				t.Errorf("Got: %v, wantErr: %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(mergedTags, expectedTags) {
				t.Errorf("Expected %+v, Got: %+v, InfraTags: %+v, MachineSpecTags: %+v",
					expectedTags, mergedTags, infraTags, machineSpecTags)
			}
		})
	}
}
