package main

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/bookingcom/shipper/pkg/controller/capacity"
	"github.com/bookingcom/shipper/pkg/controller/clustersecret"
	"github.com/bookingcom/shipper/pkg/controller/installation"
	"github.com/bookingcom/shipper/pkg/controller/schedulecontroller"
	"github.com/bookingcom/shipper/pkg/controller/shipmentorder"
	"github.com/bookingcom/shipper/pkg/controller/strategy"
	"github.com/bookingcom/shipper/pkg/controller/traffic"
)

type initFunc func(*cfg) (bool, error)

func buildInitializers() map[string]initFunc {
	controllers := map[string]initFunc{}
	controllers["shipmentorder"] = startShipmentOrderController
	controllers["clustersecret"] = startClusterSecretController
	controllers["schedule"] = startScheduleController
	controllers["strategy"] = startStrategyController
	controllers["installation"] = startInstallationController
	controllers["capacity"] = startCapacityController
	controllers["traffic"] = startTrafficController
	return controllers
}

func startShipmentOrderController(cfg *cfg) (bool, error) {
	c := shipmentorder.NewController(
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.recorder(shipmentorder.AgentName),
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}

func startClusterSecretController(cfg *cfg) (bool, error) {
	if cfg.certPath == "" && cfg.keyPath == "" {
		return false, nil
	}

	c := clustersecret.NewController(
		cfg.shipperInformerFactory,
		cfg.kubeClient,
		cfg.kubeInformerFactory,
		cfg.certPath,
		cfg.keyPath,
		cfg.ns,
		cfg.recorder(clustersecret.AgentName),
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}

func startScheduleController(cfg *cfg) (bool, error) {
	c := schedulecontroller.NewController(
		cfg.kubeClient,
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.recorder(schedulecontroller.AgentName),
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}

func startStrategyController(cfg *cfg) (bool, error) {
	// does not use a recorder yet
	c := strategy.NewController(
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		dynamic.NewDynamicClientPool(cfg.restCfg),
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}

func startInstallationController(cfg *cfg) (bool, error) {
	dynamicClientBuilderFunc := func(gvk *schema.GroupVersionKind, config *rest.Config) dynamic.Interface {
		// Probably this needs to be fixed, according to @asurikov's latest findings.
		config.APIPath = dynamic.LegacyAPIPathResolverFunc(*gvk)
		config.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}

		dynamicClient, newClientErr := dynamic.NewClient(config)
		if newClientErr != nil {
			glog.Fatal(newClientErr)
		}
		return dynamicClient
	}

	// does not use a recorder yet
	c := installation.NewController(
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.store,
		dynamicClientBuilderFunc,
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}

func startCapacityController(cfg *cfg) (bool, error) {
	c := capacity.NewController(
		cfg.kubeClient,
		cfg.shipperClient,
		cfg.kubeInformerFactory,
		cfg.shipperInformerFactory,
		cfg.store,
		cfg.recorder(capacity.AgentName),
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}

func startTrafficController(cfg *cfg) (bool, error) {
	c := traffic.NewController(
		cfg.kubeClient,
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.store,
		cfg.recorder(traffic.AgentName),
	)
	go c.Run(cfg.workers, cfg.stopCh)
	cfg.wg.Add(1)
	return true, nil
}
