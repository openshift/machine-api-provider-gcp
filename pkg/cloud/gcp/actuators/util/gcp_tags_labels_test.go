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
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	controllerfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
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
