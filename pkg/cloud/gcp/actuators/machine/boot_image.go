package machine

import (
	"encoding/json"
	"fmt"

	"github.com/coreos/stream-metadata-go/stream"
	"github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/util"
	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var osImageStreamGVK = schema.GroupVersionKind{
	Group:   "machineconfiguration.openshift.io",
	Version: "v1",
	Kind:    "OSImageStream",
}

func (r *Reconciler) resolveBootImage() (string, error) {
	arch := r.resolveArchitecture()

	image, err := r.resolveImageFromConfigMap(arch)
	if err != nil {
		klog.Warningf("Failed to resolve boot image from coreos-bootimages ConfigMap: %v, using fallback", err)
		return fallbackImage(arch), nil
	}
	if image == "" {
		klog.Warningf("No GCP image found in coreos-bootimages for arch %s, using fallback", arch)
		return fallbackImage(arch), nil
	}

	klog.V(3).Infof("Resolved boot image from coreos-bootimages ConfigMap: %s (arch: %s)", image, arch)
	return image, nil
}

func (r *Reconciler) resolveArchitecture() util.NormalizedArch {
	mt, err := r.computeService.MachineTypesGet(r.projectID, r.providerSpec.Zone, r.providerSpec.MachineType)
	if err != nil || mt == nil {
		klog.V(3).Infof("Failed to get machine type %s from GCP API, falling back to prefix-based detection", r.providerSpec.MachineType)
		return util.CPUArchitecture(r.providerSpec.MachineType)
	}

	switch mt.Architecture {
	case "ARM64":
		return util.ArchitectureArm64
	case "X86_64":
		return util.ArchitectureAmd64
	case "", "ARCHITECTURE_UNSPECIFIED":
		klog.V(3).Infof("GCP API returned no architecture for machine type %s, falling back to prefix-based detection", r.providerSpec.MachineType)
		return util.CPUArchitecture(r.providerSpec.MachineType)
	default:
		klog.Warningf("Unknown GCP architecture %q for machine type %s, falling back to prefix-based detection", mt.Architecture, r.providerSpec.MachineType)
		return util.CPUArchitecture(r.providerSpec.MachineType)
	}
}

func (r *Reconciler) resolveImageFromConfigMap(arch util.NormalizedArch) (string, error) {
	var cm corev1.ConfigMap
	if err := r.apiReader.Get(r.Context, client.ObjectKey{
		Namespace: coreOSBootImagesNamespace,
		Name:      coreOSBootImagesName,
	}, &cm); err != nil {
		return "", fmt.Errorf("failed to get coreos-bootimages ConfigMap: %w", err)
	}

	streamData, err := r.resolveStreamData(cm.Data)
	if err != nil {
		return "", err
	}

	var st stream.Stream
	if err := json.Unmarshal([]byte(streamData), &st); err != nil {
		return "", fmt.Errorf("failed to parse stream metadata: %w", err)
	}

	streamArch := archToStreamArch(arch)
	archData, ok := st.Architectures[streamArch]
	if !ok {
		return "", fmt.Errorf("no architecture %q in stream metadata", streamArch)
	}

	if archData.Images.Gcp == nil {
		return "", fmt.Errorf("no GCP image entry for architecture %q in stream metadata", streamArch)
	}

	return gcpImageReference(archData.Images.Gcp.Project, archData.Images.Gcp.Name), nil
}

func (r *Reconciler) resolveStreamData(cmData map[string]string) (string, error) {
	streamsRaw, hasStreams := cmData["streams"]
	if hasStreams {
		streamName := r.resolveActiveStreamName()
		var streams map[string]json.RawMessage
		if err := json.Unmarshal([]byte(streamsRaw), &streams); err != nil {
			return "", fmt.Errorf("failed to parse streams data from ConfigMap: %w", err)
		}

		data, ok := streams[streamName]
		if !ok {
			return "", fmt.Errorf("stream %q not found in coreos-bootimages ConfigMap streams key", streamName)
		}
		return string(data), nil
	}

	streamData, hasStream := cmData["stream"]
	if hasStream {
		klog.V(3).Info("coreos-bootimages ConfigMap missing 'streams' key, falling back to deprecated 'stream' key")
		return streamData, nil
	}

	return "", fmt.Errorf("coreos-bootimages ConfigMap missing both 'streams' and 'stream' keys")
}

func (r *Reconciler) resolveActiveStreamName() string {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(osImageStreamGVK)

	err := r.apiReader.Get(r.Context, client.ObjectKey{Name: osImageStreamName}, obj)
	if err != nil {
		if apimachineryerrors.IsNotFound(err) {
			klog.V(3).Infof("OSImageStream CR not found, defaulting to stream %q", defaultOSStreamName)
		} else {
			klog.Warningf("Failed to get OSImageStream CR: %v, defaulting to stream %q", err, defaultOSStreamName)
		}
		return defaultOSStreamName
	}

	defaultStream, found, err := unstructured.NestedString(obj.Object, "spec", "defaultStream")
	if err != nil || !found || defaultStream == "" {
		klog.V(3).Infof("OSImageStream CR has no spec.defaultStream set, defaulting to stream %q", defaultOSStreamName)
		return defaultOSStreamName
	}

	klog.V(3).Infof("Resolved active OS stream from OSImageStream CR: %s", defaultStream)
	return defaultStream
}

func archToStreamArch(arch util.NormalizedArch) string {
	switch arch {
	case util.ArchitectureArm64:
		return "aarch64"
	case util.ArchitectureAmd64:
		return "x86_64"
	default:
		return "x86_64"
	}
}

func fallbackImage(arch util.NormalizedArch) string {
	if arch == util.ArchitectureArm64 {
		return defaultGCPBootImageARM
	}
	return defaultGCPBootImageX86
}

func gcpImageReference(project, name string) string {
	return fmt.Sprintf("projects/%s/global/images/%s", project, name)
}
