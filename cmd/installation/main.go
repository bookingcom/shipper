package main

import (
	"flag"
	"time"

	"github.com/golang/glog"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller/installation"
	"k8s.io/apiserver/pkg/server"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	shipperClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building shipper clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	shipperInformerFactory := informers.NewSharedInformerFactory(shipperClient, time.Second*30)

	store := clusterclientstore.NewStore(
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory.Shipper().V1().Clusters(),
	)

	controller := installation.NewController(shipperClient, shipperInformerFactory, store)

	go kubeInformerFactory.Start(stopCh)
	go shipperInformerFactory.Start(stopCh)

	err = store.Run(stopCh)
	if err != nil {
		glog.Fatalf("Error running client store: %s", err.Error())
	}
	glog.Infof("starting controller...")
	controller.Run(2, stopCh)
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterUrl, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
