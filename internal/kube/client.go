package kube

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

type ClientFactory func() (kubernetes.Interface, error)

func NewClientFactory(configFlags *genericclioptions.ConfigFlags) ClientFactory {
	return func() (kubernetes.Interface, error) {
		config, err := configFlags.ToRESTConfig()
		if err != nil {
			return nil, err
		}
		config.UserAgent = "kubectl-ops"
		return kubernetes.NewForConfig(config)
	}
}
