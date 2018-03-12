package main

import (
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller/capacity"
	"github.com/bookingcom/shipper/pkg/controller/clustersecret"
	"github.com/bookingcom/shipper/pkg/controller/installation"
	"github.com/bookingcom/shipper/pkg/controller/schedulecontroller"
	"github.com/bookingcom/shipper/pkg/controller/shipmentorder"
	"github.com/bookingcom/shipper/pkg/controller/strategy"
	"github.com/bookingcom/shipper/pkg/controller/traffic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
)

var (
	masterURL    = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig   = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	certPath     = flag.String("cert", "", "Path to the TLS certificate for target clusters.")
	keyPath      = flag.String("key", "", "Path to the TLS private key for target clusters.")
	ns           = flag.String("namespace", "shipper-system", "Namespace for Shipper resources.")
	resyncPeriod = flag.String("resync", "30s", "Informer's cache re-sync in Go's duration format.")
	workers      = flag.Int("workers", 2, "Number of workers to start for each controller.")
)

type cfg struct {
	kubeClient        kubernetes.Interface
	shipperClient     shipperclientset.Interface
	restCfg           *rest.Config
	resync            time.Duration
	certPath, keyPath string
	ns                string
	workers           int
	stopCh            <-chan struct{}
}

func main() {
	flag.Parse()

	if *certPath == "" {
		glog.Fatal("Need -cert")
	}

	if *keyPath == "" {
		glog.Fatal("Need -key")
	}

	resync, err := time.ParseDuration(*resyncPeriod)
	if err != nil {
		glog.Fatal(err)
	}

	kubeClient, shipperClient, restCfg, err := buildClients(*masterURL, *kubeconfig)
	if err != nil {
		glog.Fatal(err)
	}

	stopCh := setupSignalHandler()

	cfg := &cfg{
		kubeClient:    kubeClient,
		shipperClient: shipperClient,
		restCfg:       restCfg,
		resync:        resync,
		certPath:      *certPath,
		keyPath:       *keyPath,
		ns:            *ns,
		workers:       *workers,
		stopCh:        stopCh,
	}

	runControllers(cfg)
}

func buildClients(masterURL, kubeconfig string) (kubernetes.Interface, shipperclientset.Interface, *rest.Config, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		return nil, nil, nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	shipperClient, err := shipperclientset.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	return kubeClient, shipperClient, restCfg, nil
}

func runControllers(cfg *cfg) {
	kubeInformerFactory := informers.NewSharedInformerFactory(cfg.kubeClient, cfg.resync)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(cfg.shipperClient, cfg.resync)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cfg.kubeClient.CoreV1().Events("")})
	shipperscheme.AddToScheme(scheme.Scheme)

	recorder := func(component string) record.EventRecorder {
		return broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: component})
	}

	var wg sync.WaitGroup

	store := clusterclientstore.NewStore(
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory.Shipper().V1().Clusters(),
	)

	type ctrl interface {
		Run(int, <-chan struct{})
	}

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

	for _, c := range []ctrl{
		shipmentorder.NewController(cfg.shipperClient, shipperInformerFactory,
			recorder(shipmentorder.AgentName)),

		clustersecret.NewController(shipperInformerFactory, cfg.kubeClient, kubeInformerFactory, cfg.certPath, cfg.keyPath, cfg.ns,
			recorder(clustersecret.AgentName)),

		// does not use a recorder yet
		installation.NewController(cfg.shipperClient, shipperInformerFactory, store, dynamicClientBuilderFunc),

		capacity.NewController(cfg.kubeClient, cfg.shipperClient, kubeInformerFactory, shipperInformerFactory, store,
			recorder(capacity.AgentName)),

		traffic.NewController(cfg.kubeClient, cfg.shipperClient, shipperInformerFactory, store,
			recorder(traffic.AgentName)),

		// does not use a recorder yet
		strategy.NewController(cfg.shipperClient, shipperInformerFactory, dynamic.NewDynamicClientPool(cfg.restCfg)),

		schedulecontroller.NewController(cfg.kubeClient, cfg.shipperClient, shipperInformerFactory,
			recorder(schedulecontroller.AgentName)),
	} {
		wg.Add(1)

		go func(c ctrl) {
			c.Run(2, cfg.stopCh)
			wg.Done()
		}(c)
	}

	go kubeInformerFactory.Start(cfg.stopCh)
	go shipperInformerFactory.Start(cfg.stopCh)

	if err := store.Run(cfg.stopCh); err != nil {
		glog.Fatalf("Error running client store: %s", err)
	}

	// TODO make it visible when some controller's aren't starting properly

	wg.Wait()
}

func setupSignalHandler() <-chan struct{} {
	stopCh := make(chan struct{})

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		close(stopCh)
		<-c
		os.Exit(1) // Second signal. Exit directly.
	}()

	return stopCh
}
