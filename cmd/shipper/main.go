package main

import (
	"flag"
	"os"
	"os/signal"
	"strings"
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

var controllers = []string{
	"shipmentorder",
	"clustersecret",
	"schedule",
	"strategy",
	"installation",
	"capacity",
	"traffic",
}

var (
	masterURL           = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig          = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	certPath            = flag.String("cert", "", "Path to the TLS certificate for target clusters.")
	keyPath             = flag.String("key", "", "Path to the TLS private key for target clusters.")
	ns                  = flag.String("namespace", "shipper-system", "Namespace for Shipper resources.")
	resyncPeriod        = flag.String("resync", "30s", "Informer's cache re-sync in Go's duration format.")
	enabledControllers  = flag.String("enable", strings.Join(controllers, ","), "comma-seperated list of controllers to run (if not all)")
	disabledControllers = flag.String("disable", "", "comma-seperated list of controllers to disable")
	workers             = flag.Int("workers", 2, "Number of workers to start for each controller.")
)

type cfg struct {
	enabledControllers map[string]bool

	restCfg *rest.Config

	kubeClient             kubernetes.Interface
	shipperClient          shipperclientset.Interface
	kubeInformerFactory    informers.SharedInformerFactory
	shipperInformerFactory shipperinformers.SharedInformerFactory
	resync                 time.Duration

	recorder func(string) record.EventRecorder

	store *clusterclientstore.Store

	certPath, keyPath string
	ns                string
	workers           int

	wg     *sync.WaitGroup
	stopCh <-chan struct{}
}

func main() {
	flag.Parse()

	resync, err := time.ParseDuration(*resyncPeriod)
	if err != nil {
		glog.Fatal(err)
	}

	kubeClient, shipperClient, restCfg, err := buildClients(*masterURL, *kubeconfig)
	if err != nil {
		glog.Fatal(err)
	}

	stopCh := setupSignalHandler()

	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, resync)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperClient, resync)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	shipperscheme.AddToScheme(scheme.Scheme)

	recorder := func(component string) record.EventRecorder {
		return broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: component})
	}

	enabledControllers := buildEnabledControllers(*enabledControllers, *disabledControllers)
	if enabledControllers["clustersecret"] {
		if *certPath == "" || *keyPath == "" {
			glog.Fatal("--cert and --key must both be specified if the clustersecret controller is running")
		}
	}

	wg := &sync.WaitGroup{}

	store := clusterclientstore.NewStore(
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory.Shipper().V1().Clusters(),
	)

	wg.Add(1)
	go func() {
		store.Run(stopCh)
		wg.Done()
	}()

	cfg := &cfg{
		enabledControllers: enabledControllers,
		restCfg:            restCfg,

		kubeClient:             kubeClient,
		shipperClient:          shipperClient,
		kubeInformerFactory:    kubeInformerFactory,
		shipperInformerFactory: shipperInformerFactory,
		resync:                 resync,

		recorder: recorder,

		store: store,

		certPath: *certPath,
		keyPath:  *keyPath,
		ns:       *ns,
		workers:  *workers,

		wg:     wg,
		stopCh: stopCh,
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

func buildEnabledControllers(enabledControllers, disabledControllers string) map[string]bool {
	willRun := map[string]bool{}
	for _, controller := range controllers {
		willRun[controller] = false
	}

	userEnabled := strings.Split(enabledControllers, ",")
	for _, controller := range userEnabled {
		if controller == "" {
			continue
		}

		_, ok := willRun[controller]
		if !ok {
			glog.Fatalf("cannot enable %q: it is not a known controller", controller)
		}
		willRun[controller] = true
	}

	userDisabled := strings.Split(disabledControllers, ",")
	for _, controller := range userDisabled {
		if controller == "" {
			continue
		}

		_, ok := willRun[controller]
		if !ok {
			glog.Fatalf("cannot disable %q: it is not a known controller", controller)
		}

		willRun[controller] = false
	}

	return willRun
}

func runControllers(cfg *cfg) {
	controllerInitializers := buildInitializers()

	for name, initializer := range controllerInitializers {
		started, err := initializer(cfg)
		// TODO make it visible when some controller's aren't starting properly; all of the initializers return 'nil' ATM
		if err != nil {
			glog.Fatalf("%q failed to initialize", name)
		}

		if !started {
			glog.Infof("%q was skipped per config", name)
		}
	}

	go cfg.kubeInformerFactory.Start(cfg.stopCh)
	go cfg.shipperInformerFactory.Start(cfg.stopCh)

	doneCh := make(chan struct{})

	go func() {
		cfg.wg.Wait()
		close(doneCh)
	}()

	<-doneCh
	glog.Info("Controllers have shut down")
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
	enabled := cfg.enabledControllers["shipmentorder"]
	if !enabled {
		return false, nil
	}

	c := shipmentorder.NewController(
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.recorder(shipmentorder.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startClusterSecretController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["clustersecret"]
	if !enabled {
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

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startScheduleController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["schedule"]
	if !enabled {
		return false, nil
	}

	c := schedulecontroller.NewController(
		cfg.kubeClient,
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.recorder(schedulecontroller.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startStrategyController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["strategy"]
	if !enabled {
		return false, nil
	}

	// does not use a recorder yet
	c := strategy.NewController(
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		dynamic.NewDynamicClientPool(cfg.restCfg),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

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

	enabled := cfg.enabledControllers["installation"]
	if !enabled {
		return false, nil
	}

	// does not use a recorder yet
	c := installation.NewController(
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.store,
		dynamicClientBuilderFunc,
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startCapacityController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["capacity"]
	if !enabled {
		return false, nil
	}

	c := capacity.NewController(
		cfg.kubeClient,
		cfg.shipperClient,
		cfg.kubeInformerFactory,
		cfg.shipperInformerFactory,
		cfg.store,
		cfg.recorder(capacity.AgentName),
	)
	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()
	return true, nil
}

func startTrafficController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["traffic"]
	if !enabled {
		return false, nil
	}

	c := traffic.NewController(
		cfg.kubeClient,
		cfg.shipperClient,
		cfg.shipperInformerFactory,
		cfg.store,
		cfg.recorder(traffic.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}
