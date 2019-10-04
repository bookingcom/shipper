package kubeconfig

import (
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
)

func RegisterFlag(flags *pflag.FlagSet, kubeConfigFile *string) {
	flags.StringVar(kubeConfigFile, "kube-config", "~/.kube/config", "the path to the Kubernetes configuration file")
}

func Load(context, kubeConfigFile string) (clientcmd.ClientConfig, error) {
	kubeConfigFilePath, err := homedir.Expand(kubeConfigFile)
	if err != nil {
		return nil, err
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigFilePath},
		&clientcmd.ConfigOverrides{CurrentContext: context},
	)

	return clientConfig, nil
}
