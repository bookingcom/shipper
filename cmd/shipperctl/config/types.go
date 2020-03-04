package config

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ClustersConfiguration struct {
	ApplicationClusters []*ClusterConfiguration `yaml:"applicationClusters"`
}

type ClusterConfiguration struct {
	Name                string `yaml:"name"`
	Context             string `yaml:"context"`
	shipper.ClusterSpec `yaml:",inline"`
}
