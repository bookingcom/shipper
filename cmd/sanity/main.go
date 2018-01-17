package main

import (
	"flag"
	"fmt"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
)

var (
	kuberconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	master      = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
)

// the sanity command ensures that we can use our generated clients to fetch things from the cluster
func main() {
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags(*master, *kuberconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %v", err)
	}

	shipperClient, err := shipperclientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building example clientset: %v", err)
	}

	list, err := shipperClient.ShipperV1().ShipmentOrders("default").List(metav1.ListOptions{})
	if err != nil {
		glog.Fatalf("Error listing all ShipmentOrders %v", err)
	}

	for _, so := range list.Items {
		fmt.Printf("ShipmentOrder %s with strategy %q\n", so.Name, so.Spec.Strategy)
	}
}
