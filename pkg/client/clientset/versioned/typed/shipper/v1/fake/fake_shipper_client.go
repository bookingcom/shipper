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

package fake

import (
	v1 "github.com/bookingcom/shipper/pkg/client/clientset/versioned/typed/shipper/v1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeShipperV1 struct {
	*testing.Fake
}

func (c *FakeShipperV1) Applications(namespace string) v1.ApplicationInterface {
	return &FakeApplications{c, namespace}
}

func (c *FakeShipperV1) CapacityTargets(namespace string) v1.CapacityTargetInterface {
	return &FakeCapacityTargets{c, namespace}
}

func (c *FakeShipperV1) Clusters() v1.ClusterInterface {
	return &FakeClusters{c}
}

func (c *FakeShipperV1) InstallationTargets(namespace string) v1.InstallationTargetInterface {
	return &FakeInstallationTargets{c, namespace}
}

func (c *FakeShipperV1) Releases(namespace string) v1.ReleaseInterface {
	return &FakeReleases{c, namespace}
}

func (c *FakeShipperV1) Strategies(namespace string) v1.StrategyInterface {
	return &FakeStrategies{c, namespace}
}

func (c *FakeShipperV1) TrafficTargets(namespace string) v1.TrafficTargetInterface {
	return &FakeTrafficTargets{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeShipperV1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
