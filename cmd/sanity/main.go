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
	resourceType = flag.String("type", "app", "short name for the type you want to query")
	kuberconfig  = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	master       = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
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

	switch *resourceType {
	case "app":
		list, err := shipperClient.ShipperV1().Applications("default").List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error listing %q: %v", *resourceType, err)
		}

		for _, item := range list.Items {
			fmt.Printf("%s %q with this spec: %+v\n", item.Kind, item.Name, item.Spec)
		}
	case "rel":
		list, err := shipperClient.ShipperV1().Releases("default").List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error listing %q: %v", *resourceType, err)
		}

		for _, item := range list.Items {
			fmt.Printf("%s %q with this spec: %+v and this environment: %+v\n", item.Kind, item.Name, item.Spec, item.Environment)
		}
	case "clusters":
		list, err := shipperClient.ShipperV1().Clusters().List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error listing %q: %v", *resourceType, err)
		}

		for _, item := range list.Items {
			fmt.Printf("%s %q with this spec: %+v\n", item.Kind, item.Name, item.Spec)
		}
	case "it":
		list, err := shipperClient.ShipperV1().InstallationTargets("default").List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error listing %q: %v", *resourceType, err)
		}

		for _, item := range list.Items {
			fmt.Printf("%s %q with this spec: %+v\n", item.Kind, item.Name, item.Spec)
		}
	case "ct":
		list, err := shipperClient.ShipperV1().CapacityTargets("default").List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error listing %q: %v", *resourceType, err)
		}

		for _, item := range list.Items {
			fmt.Printf("%s %q with this spec: %+v\n", item.Kind, item.Name, item.Spec)
		}
	case "tt":
		list, err := shipperClient.ShipperV1().TrafficTargets("default").List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error listing %q: %v", *resourceType, err)
		}

		for _, item := range list.Items {
			fmt.Printf("%s %q with this spec: %+v\n", item.Kind, item.Name, item.Spec)
		}
	default:
		glog.Fatalf("unknown resource short name %q. try one of: app, so, rel, it, ct, tt", *resourceType)
	}
}
