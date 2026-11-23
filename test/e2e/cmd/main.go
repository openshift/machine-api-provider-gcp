package main

import (
	"fmt"
	"os"
	"time"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	"github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	"github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	"github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/spf13/cobra"

	_ "github.com/openshift/machine-api-provider-gcp/test/e2e/tests"
)

func newExtension() (*extension.Extension, error) {
	ext := extension.NewExtension("openshift", "payload", "machine-api-provider-gcp")

	// Allow GCP provisioning and the original cleanup waits.
	timeout := 75 * time.Minute
	ext.AddSuite(extension.Suite{
		Name:             "machine-api-provider-gcp/periodic",
		Description:      "GCP provider-specific Machine API tests",
		Qualifiers:       []string{`labels.exists(l, l == "periodic") && labels.exists(l, l == "mapi")`},
		Parallelism:      1,
		ClusterStability: extension.ClusterStabilityDisruptive,
		TestTimeout:      &timeout,
	})

	specs, err := ginkgo.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		return nil, fmt.Errorf("build GCP extension test specs: %w", err)
	}
	specs.Include(extensiontests.PlatformEquals("gcp"))
	ext.AddSpecs(specs)
	return ext, nil
}

func main() {
	ext, err := newExtension()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	registry := extension.NewRegistry()
	registry.Register(ext)

	root := &cobra.Command{Long: "Machine API Provider GCP test extension"}
	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
