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

var (
	// testTagSets contains tags for tests which can be converted to
	// machinev1.ResourceManagerTag and configv1.GCPResourceTag types.
	testTagSets = []testResourceManagerTag{
		{
			ParentID: "openshift",
			Key:      "key1",
			Value:    "value1"},
		{
			ParentID: "openshift",
			Key:      "key2",
			Value:    "value2"},
		{
			ParentID: "openshift",
			Key:      "key3",
			Value:    "value3"},
		{
			ParentID: "openshift",
			Key:      "key4",
			Value:    "value4"},
		{
			ParentID: "openshift",
			Key:      "key5",
			Value:    "value5"},
		{
			ParentID: "openshift",
			Key:      "key6",
			Value:    "value6"},
		{
			ParentID: "openshift",
			Key:      "key7",
			Value:    "value7"},
		{
			ParentID: "openshift",
			Key:      "key8",
			Value:    "value8"},
		{
			ParentID: "openshift",
			Key:      "key9",
			Value:    "value9"},
		{
			ParentID: "openshift",
			Key:      "key10",
			Value:    "value10"},
		{
			ParentID: "openshift",
			Key:      "key11",
			Value:    "value11"},
		{
			ParentID: "openshift",
			Key:      "key12",
			Value:    "value12"},
		{
			ParentID: "openshift",
			Key:      "key13",
			Value:    "value13"},
		{
			ParentID: "openshift",
			Key:      "key14",
			Value:    "value14"},
		{
			ParentID: "openshift",
			Key:      "key15",
			Value:    "value15"},
		{
			ParentID: "openshift",
			Key:      "key16",
			Value:    "value16"},
		{
			ParentID: "openshift",
			Key:      "key17",
			Value:    "value17"},
		{
			ParentID: "openshift",
			Key:      "key18",
			Value:    "value18"},
		{
			ParentID: "openshift",
			Key:      "key19",
			Value:    "value19"},
		{
			ParentID: "openshift",
			Key:      "key20",
			Value:    "value20"},
		{
			ParentID: "openshift",
			Key:      "key21",
			Value:    "value21"},
		{
			ParentID: "openshift",
			Key:      "key22",
			Value:    "value22"},
		{
			ParentID: "openshift",
			Key:      "key23",
			Value:    "value23"},
		{
			ParentID: "openshift",
			Key:      "key24",
			Value:    "value24"},
		{
			ParentID: "openshift",
			Key:      "key25",
			Value:    "value25"},
		{
			ParentID: "openshift",
			Key:      "key26",
			Value:    "value26"},
		{
			ParentID: "openshift",
			Key:      "key27",
			Value:    "value27"},
		{
			ParentID: "openshift",
			Key:      "key28",
			Value:    "value28"},
		{
			ParentID: "openshift",
			Key:      "key29",
			Value:    "value29"},
		{
			ParentID: "openshift",
			Key:      "key30",
			Value:    "value30"},
		{
			ParentID: "openshift",
			Key:      "key31",
			Value:    "value31"},
		{
			ParentID: "openshift",
			Key:      "key32",
			Value:    "value32"},
		{
			ParentID: "openshift",
			Key:      "key33",
			Value:    "value33"},
		{
			ParentID: "openshift",
			Key:      "key34",
			Value:    "value34"},
		{
			ParentID: "openshift",
			Key:      "key35",
			Value:    "value35"},
		{
			ParentID: "openshift",
			Key:      "key36",
			Value:    "value36"},
		{
			ParentID: "openshift",
			Key:      "key37",
			Value:    "value37"},
		{
			ParentID: "openshift",
			Key:      "key38",
			Value:    "value38"},
		{
			ParentID: "openshift",
			Key:      "key39",
			Value:    "value39"},
		{
			ParentID: "openshift",
			Key:      "key40",
			Value:    "value40"},
		{
			ParentID: "openshift",
			Key:      "key41",
			Value:    "value41"},
		{
			ParentID: "openshift",
			Key:      "key42",
			Value:    "value42"},
		{
			ParentID: "openshift",
			Key:      "key43",
			Value:    "value43"},
		{
			ParentID: "openshift",
			Key:      "key44",
			Value:    "value44"},
		{
			ParentID: "openshift",
			Key:      "key45",
			Value:    "value45"},
		{
			ParentID: "openshift",
			Key:      "key46",
			Value:    "value46"},
		{
			ParentID: "openshift",
			Key:      "key47",
			Value:    "value47"},
		{
			ParentID: "openshift",
			Key:      "key48",
			Value:    "value48"},
		{
			ParentID: "openshift",
			Key:      "key49",
			Value:    "value49"},
		{
			ParentID: "openshift",
			Key:      "key50",
			Value:    "value50"},
		{
			ParentID: "openshift",
			Key:      "key51",
			Value:    "value51"},
		{
			ParentID: "openshift",
			Key:      "key52",
			Value:    "value52"},
		{
			ParentID: "openshift",
			Key:      "key101",
			Value:    "value101"},
		{
			ParentID: "openshift",
			Key:      "key501",
			Value:    "value501"},
	}

	// fakeTagsKeyValueNameMap contains the tag Key(`tagKeys/{tag_key_id}`) and
	// Value(`tagValues/{tag_value_id}`) Names mapped with the tag's NamespacedName
	// (`{parentId}/{tagKeyShort}/{tagValueShort}`).
	fakeTagsKeyValueNameMap = map[string]struct {
		key   string
		value string
	}{
		"openshift/key1/value1": {
			key:   "tagKeys/0076293608",
			value: "tagValues/0055629312"},
		"openshift/key2/value2": {
			key:   "tagKeys/0023033386",
			value: "tagValues/0088842980"},
		"openshift/key3/value3": {
			key:   "tagKeys/0069899625",
			value: "tagValues/0047417661"},
		"openshift/key4/value4": {
			key:   "tagKeys/0050205103",
			value: "tagValues/0036307885"},
		"openshift/key5/value5": {
			key:   "tagKeys/0079771629",
			value: "tagValues/0065673174"},
		"openshift/key6/value6": {
			key:   "tagKeys/0051626174",
			value: "tagValues/0027708252"},
		"openshift/key7/value7": {
			key:   "tagKeys/0030414034",
			value: "tagValues/0020489890"},
		"openshift/key8/value8": {
			key:   "tagKeys/0050469265",
			value: "tagValues/0002904904"},
		"openshift/key9/value9": {
			key:   "tagKeys/0076293608",
			value: "tagValues/0055629312"},
		"openshift/key10/value10": {
			key:   "tagKeys/0023033386",
			value: "tagValues/0088842980"},
		"openshift/key11/value11": {
			key:   "tagKeys/0069899625",
			value: "tagValues/0047417661"},
		"openshift/key12/value12": {
			key:   "tagKeys/0034605069",
			value: "tagValues/0050384905"},
		"openshift/key13/value13": {
			key:   "tagKeys/0028357547",
			value: "tagValues/0052268968"},
		"openshift/key14/value14": {
			key:   "tagKeys/0099944474",
			value: "tagValues/0059052883"},
		"openshift/key15/value15": {
			key:   "tagKeys/0050205103",
			value: "tagValues/0036307885"},
		"openshift/key16/value16": {
			key:   "tagKeys/0079771629",
			value: "tagValues/0065673174"},
		"openshift/key17/value17": {
			key:   "tagKeys/0060225722",
			value: "tagValues/0081145498"},
		"openshift/key18/value18": {
			key:   "tagKeys/0016496476",
			value: "tagValues/0046494994"},
		"openshift/key19/value19": {
			key:   "tagKeys/0093247819",
			value: "tagValues/0041540373"},
		"openshift/key20/value20": {
			key:   "tagKeys/0080859513",
			value: "tagValues/0016693395"},
		"openshift/key21/value21": {
			key:   "tagKeys/0018537779",
			value: "tagValues/0003454649"},
		"openshift/key22/value22": {
			key:   "tagKeys/0071724280",
			value: "tagValues/0047292544"},
		"openshift/key23/value23": {
			key:   "tagKeys/0045095645",
			value: "tagValues/0089378558"},
		"openshift/key24/value24": {
			key:   "tagKeys/0044575217",
			value: "tagValues/0022754275"},
		"openshift/key25/value25": {
			key:   "tagKeys/0056084774",
			value: "tagValues/0040808197"},
		"openshift/key26/value26": {
			key:   "tagKeys/0086508506",
			value: "tagValues/0091979350"},
		"openshift/key27/value27": {
			key:   "tagKeys/0085330359",
			value: "tagValues/0051833259"},
		"openshift/key28/value28": {
			key:   "tagKeys/0094744916",
			value: "tagValues/0011642000"},
		"openshift/key29/value29": {
			key:   "tagKeys/0014270555",
			value: "tagValues/0072404680"},
		"openshift/key30/value30": {
			key:   "tagKeys/0079085850",
			value: "tagValues/0007793185"},
		"openshift/key31/value31": {
			key:   "tagKeys/0031484153",
			value: "tagValues/0050294705"},
		"openshift/key32/value32": {
			key:   "tagKeys/0045311563",
			value: "tagValues/0029329808"},
		"openshift/key33/value33": {
			key:   "tagKeys/0080836115",
			value: "tagValues/0003514535"},
		"openshift/key34/value34": {
			key:   "tagKeys/0072216154",
			value: "tagValues/0060486146"},
		"openshift/key35/value35": {
			key:   "tagKeys/0025032284",
			value: "tagValues/0038979234"},
		"openshift/key36/value36": {
			key:   "tagKeys/0057998529",
			value: "tagValues/0067716498"},
		"openshift/key37/value37": {
			key:   "tagKeys/0086808493",
			value: "tagValues/0060060909"},
		"openshift/key38/value38": {
			key:   "tagKeys/0029402635",
			value: "tagValues/0060648494"},
		"openshift/key39/value39": {
			key:   "tagKeys/0034805062",
			value: "tagValues/0066064364"},
		"openshift/key40/value40": {
			key:   "tagKeys/0052208445",
			value: "tagValues/0098376015"},
		"openshift/key41/value41": {
			key:   "tagKeys/0078738427",
			value: "tagValues/0046807958"},
		"openshift/key42/value42": {
			key:   "tagKeys/0075526710",
			value: "tagValues/0092960786"},
		"openshift/key43/value43": {
			key:   "tagKeys/0071238172",
			value: "tagValues/0014715775"},
		"openshift/key44/value44": {
			key:   "tagKeys/0098580341",
			value: "tagValues/0052744277"},
		"openshift/key45/value45": {
			key:   "tagKeys/0046775969",
			value: "tagValues/0095864916"},
		"openshift/key46/value46": {
			key:   "tagKeys/0041543652",
			value: "tagValues/0054302656"},
		"openshift/key47/value47": {
			key:   "tagKeys/0043236402",
			value: "tagValues/0087723355"},
		"openshift/key48/value48": {
			key:   "tagKeys/0098289690",
			value: "tagValues/0018491792"},
		"openshift/key49/value49": {
			key:   "tagKeys/0022125412",
			value: "tagValues/0063407483"},
		"openshift/key50/value50": {
			key:   "tagKeys/0004322107",
			value: "tagValues/0083960428"},
		"openshift/key51/value51": {
			key:   "tagKeys/0070391549",
			value: "tagValues/0027514043"},
		"openshift/key51/value52": {
			key:   "tagKeys/0070391542",
			value: "tagValues/0227514043"},
		"openshift/key1/value101": {
			key:   "tagKeys/0076293608",
			value: "tagValues/0081145498"},
		"openshift/key5/value501": {
			key:   "tagKeys/0079771629",
			value: "tagValues/0081501498"},
	}
)

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
		name                 string
		labelsFeatureEnabled bool
		getInfra             func() *configv1.Infrastructure
		machineSpecLabels    map[string]string
		expectedLabels       map[string]string
		wantErr              bool
	}{
		{
			name:                 "Infrastructure resource doesn't exist, feature is enabled",
			labelsFeatureEnabled: true,
			getInfra:             func() *configv1.Infrastructure { return nil },
			machineSpecLabels:    nil,
			expectedLabels:       nil,
			wantErr:              true,
		},
		{
			name:                 "user-defined labels is empty",
			labelsFeatureEnabled: true,
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
			name:                 "user-defined labels feature is disabled",
			labelsFeatureEnabled: false,
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
				"key3": "value3", "key4": "value4",
				fmt.Sprintf("kubernetes-io-cluster-%s", machineClusterID): "owned",
			},
			wantErr: false,
		},
		{
			name:                 "user-defined labels feature is enabled",
			labelsFeatureEnabled: true,
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
			name:                 "providerSpec labels is empty",
			labelsFeatureEnabled: true,
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
			name:                 "providerSpec labels maxes allowed limit",
			labelsFeatureEnabled: true,
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
			name:                 "user-defined labels maxes allowed limit",
			labelsFeatureEnabled: true,
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
			name:                 "providerSpec and user-defined labels have duplicates",
			labelsFeatureEnabled: true,
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

			labels, err := GetLabelsList(tc.labelsFeatureEnabled, fakeClient,
				machineClusterID, tc.machineSpecLabels)

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
		getInfraObj = func(infraRef *configv1.Infrastructure, tags []testResourceManagerTag) *configv1.Infrastructure {
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
				tagKV := fakeTagsKeyValueNameMap[tag]
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

		tag, ok := fakeTagsKeyValueNameMap[name]
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
