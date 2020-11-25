package config

import (
	"github.com/spf13/pflag"

	"k8s.io/client-go/kubernetes"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
)

func RegisterFlag(flags *pflag.FlagSet, kubeConfigFile *string) {
	flags.StringVar(kubeConfigFile, "kubeconfig", "~/.kube/config", "the path to the Kubernetes configuration file")
}

func Load(kubeConfigFile, managementClusterContext string) (kubernetes.Interface, shipperclientset.Interface, error) {
	kubeClient, err := configurator.NewKubeClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return nil, nil, err
	}

	shipperClient, err := configurator.NewShipperClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return nil, nil, err
	}
	return kubeClient, shipperClient, nil
}
