/*
Copyright 2018 The Kubernetes Authors.

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

package v1

import (
	v1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	rest "k8s.io/client-go/rest"
)

type ShipperV1Interface interface {
	RESTClient() rest.Interface
	CapacityTargetsGetter
	ClustersGetter
	InstallationTargetsGetter
	ReleasesGetter
	ShipmentOrdersGetter
	StrategiesGetter
	TrafficTargetsGetter
}

// ShipperV1Client is used to interact with features provided by the shipper group.
type ShipperV1Client struct {
	restClient rest.Interface
}

func (c *ShipperV1Client) CapacityTargets(namespace string) CapacityTargetInterface {
	return newCapacityTargets(c, namespace)
}

func (c *ShipperV1Client) Clusters() ClusterInterface {
	return newClusters(c)
}

func (c *ShipperV1Client) InstallationTargets(namespace string) InstallationTargetInterface {
	return newInstallationTargets(c, namespace)
}

func (c *ShipperV1Client) Releases(namespace string) ReleaseInterface {
	return newReleases(c, namespace)
}

func (c *ShipperV1Client) ShipmentOrders(namespace string) ShipmentOrderInterface {
	return newShipmentOrders(c, namespace)
}

func (c *ShipperV1Client) Strategies(namespace string) StrategyInterface {
	return newStrategies(c, namespace)
}

func (c *ShipperV1Client) TrafficTargets(namespace string) TrafficTargetInterface {
	return newTrafficTargets(c, namespace)
}

// NewForConfig creates a new ShipperV1Client for the given config.
func NewForConfig(c *rest.Config) (*ShipperV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &ShipperV1Client{client}, nil
}

// NewForConfigOrDie creates a new ShipperV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *ShipperV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new ShipperV1Client for the given RESTClient.
func New(c rest.Interface) *ShipperV1Client {
	return &ShipperV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *ShipperV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
