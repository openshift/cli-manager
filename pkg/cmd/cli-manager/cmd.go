package cli_manager

import (
	"context"
	"os"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cli-manager/pkg/version"
)

const (
	podNameEnv      = "POD_NAME"
	podNamespaceEnv = "POD_NAMESPACE"
)

func NewCLIManagerCommand(name string) *cobra.Command {
	cmd := controllercmd.NewControllerCommandConfig("cli-manager", version.Get(), RunCLIManager).
		WithComponentOwnerReference(&corev1.ObjectReference{
			Kind:      "Pod",
			Name:      os.Getenv(podNameEnv),
			Namespace: getNamespace(),
		}).
		NewCommandWithContext(context.Background())
	cmd.Use = name
	cmd.Short = "Start the CLI manager controllers"

	return cmd
}

// getNamespace returns in-cluster namespace
func getNamespace() string {
	if nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return string(nsBytes)
	}
	if podNamespace := os.Getenv(podNamespaceEnv); len(podNamespace) > 0 {
		return podNamespace
	}
	return "openshift-cli-manager-operator"
}
