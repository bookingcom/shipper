package main

import (
	"flag"
	"time"

	"github.com/golang/glog"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller/installation"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

	dynamicClientBuilderFunc := func(gvk *schema.GroupVersionKind, config *rest.Config) dynamic.Interface {
		config.APIPath = dynamic.LegacyAPIPathResolverFunc(*gvk)
		config.ContentConfig = dynamic.ContentConfig()
		config.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}

		dynamicClient, newClientErr := dynamic.NewClient(config)
		if newClientErr != nil {
			glog.Fatal(newClientErr)
		}
		return dynamicClient
	}

	controller := installation.NewController(
		shipperClient, shipperInformerFactory, store, dynamicClientBuilderFunc)

	go kubeInformerFactory.Start(stopCh)
	go shipperInformerFactory.Start(stopCh)

	store.Run(stopCh)

	glog.Infof("starting controller...")
	controller.Run(2, stopCh)
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterUrl, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
