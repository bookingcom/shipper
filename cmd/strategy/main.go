package main

import (
	"flag"
	"github.com/golang/glog"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/controller/strategy"
	_ "github.com/bookingcom/shipper/pkg/controller/strategy"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	flag.Parse()

	stopCh := setupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	shipperclientset, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building shipper clientset: %s", err.Error())
	}

	shipperInformerFactory := informers.NewSharedInformerFactory(shipperclientset, time.Second*30)

	dynamicClientPool := dynamic.NewDynamicClientPool(cfg)

	controller := strategy.NewController(shipperclientset, shipperInformerFactory, dynamicClientPool)

	go shipperInformerFactory.Start(stopCh)

	glog.Infof("Starting controller...")
	if err = controller.Run(2, stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides value in kubeconfig. Only required if out-of-cluster.")
}

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

var onlyOneSignalHandler = make(chan struct{})

func setupSignalHandler() (stopCh <-chan struct{}) {
	close(onlyOneSignalHandler) // panics when called twice

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return stop
}
