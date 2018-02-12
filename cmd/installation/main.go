package main

import (
	"flag"
	"github.com/golang/glog"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/controller/installation"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/tools/clientcmd"
	"time"
)

var (
	kubeconfig string
	masterUrl  string
)

func main() {
	flag.Parse()

	stopCh := server.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterUrl, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	shipperClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building shipper clientset: %s", err.Error())
	}

	shipperInformerFactory := informers.NewSharedInformerFactory(shipperClient, time.Second*30)

	controller := installation.NewController(shipperClient, shipperInformerFactory)

	go shipperInformerFactory.Start(stopCh)

	glog.Infof("starting controller...")
	if err = controller.Run(2, stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterUrl, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
