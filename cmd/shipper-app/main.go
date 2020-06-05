package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	kuberestmetrics "k8s.io/client-go/tools/metrics"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart/repo"
	"github.com/bookingcom/shipper/pkg/client"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller/capacity"
	"github.com/bookingcom/shipper/pkg/controller/installation"
	"github.com/bookingcom/shipper/pkg/controller/traffic"
	"github.com/bookingcom/shipper/pkg/metrics/instrumentedclient"
	shippermetrics "github.com/bookingcom/shipper/pkg/metrics/prometheus"
	statemetrics "github.com/bookingcom/shipper/pkg/metrics/state"
)

var controllers = []string{
	"installation",
	"capacity",
	"traffic",
}

const defaultRESTTimeout time.Duration = 10 * time.Second
const defaultResync time.Duration = 0 * time.Second

var (
	masterURL           = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig          = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	ns                  = flag.String("namespace", shipper.ShipperNamespace, "Namespace for Shipper resources.")
	enabledControllers  = flag.String("enable", strings.Join(controllers, ","), "comma-seperated list of controllers to run (if not all)")
	disabledControllers = flag.String("disable", "", "comma-seperated list of controllers to disable")
	workers             = flag.Int("workers", 2, "Number of workers to start for each controller.")
	metricsAddr         = flag.String("metrics-addr", ":8889", "Addr to expose /metrics on.")
	chartCacheDir       = flag.String("cachedir", filepath.Join(os.TempDir(), "chart-cache"), "location for the local cache of downloaded charts")
	resync              = flag.Duration("resync", defaultResync, "Informer's cache re-sync in Go's duration format.")
	restTimeout         = flag.Duration("rest-timeout", defaultRESTTimeout, "Timeout value for management and target REST clients. Does not affect informer watches.")
)

type metricsCfg struct {
	readyCh chan struct{}

	wqMetrics    *shippermetrics.PrometheusWorkqueueProvider
	restLatency  *shippermetrics.RESTLatencyMetric
	restResult   *shippermetrics.RESTResultMetric
	stateMetrics statemetrics.AppMetrics
}

type cfg struct {
	enabledControllers map[string]bool

	restCfg     *rest.Config
	restTimeout *time.Duration

	kubeInformerFactory    informers.SharedInformerFactory
	shipperInformerFactory shipperinformers.SharedInformerFactory
	resync                 *time.Duration

	recorder func(string) record.EventRecorder

	store *clusterclientstore.Store

	chartVersionResolver repo.ChartVersionResolver
	chartFetcher         repo.ChartFetcher

	certPath, keyPath string
	ns                string
	workers           int

	wg     *sync.WaitGroup
	stopCh <-chan struct{}

	metrics *metricsCfg
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	restCfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		klog.Fatal(err)
	}

	// These are only used in shared informers. Setting HTTP timeout here would
	// affect watches which is undesirable. Instead, we leave it to client-go (see
	// k8s.io/client-go/tools/cache) to govern watch durations.
	informerKubeClient := client.NewKubeClientOrDie("kube-shared-informer", restCfg)
	informerShipperClient := client.NewShipperClientOrDie("shipper-shared-informer", restCfg)

	stopCh := setupSignalHandler()
	metricsReadyCh := make(chan struct{})

	kubeInformerFactory := informers.NewSharedInformerFactory(informerKubeClient, 0*time.Second)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(informerShipperClient, *resync)

	shipperscheme.AddToScheme(scheme.Scheme)

	kubeClient := client.NewKubeClientOrDie("event-broadcaster", restCfg)
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events("")})

	recorder := func(component string) record.EventRecorder {
		return broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: component})
	}

	enabledControllers := buildEnabledControllers(*enabledControllers, *disabledControllers)

	wg := &sync.WaitGroup{}
	var store *clusterclientstore.Store

	if !enabledControllers["installation"] {
		secretInformer := corev1informers.New(kubeInformerFactory, *ns, nil).Secrets()
		store = clusterclientstore.NewStore(
			func(clusterName string, ua string, config *rest.Config) (kubernetes.Interface, error) {
				return client.NewKubeClient(ua, config)
			},
			func(_, ua string, config *rest.Config) (shipperclientset.Interface, error) {
				return client.NewShipperClient(ua, config)
			},
			secretInformer,
			shipperInformerFactory,
			*ns,
			restTimeout,
		)

		wg.Add(1)
		go func() {
			store.Run(stopCh)
			wg.Done()
		}()
	}

	klog.V(1).Infof("Chart cache stored at %q", *chartCacheDir)
	klog.V(1).Infof("REST client timeout is %s", *restTimeout)

	repoCatalog := repo.NewCatalog(
		repo.DefaultFileCacheFactory(*chartCacheDir),
		repo.DefaultRemoteFetcher,
		stopCh,
	)

	ssm := statemetrics.AppMetrics{
		ItsLister: shipperInformerFactory.Shipper().V1alpha1().InstallationTargets().Lister(),
		CtsLister: shipperInformerFactory.Shipper().V1alpha1().CapacityTargets().Lister(),
		TtsLister: shipperInformerFactory.Shipper().V1alpha1().TrafficTargets().Lister(),

		NssLister: kubeInformerFactory.Core().V1().Namespaces().Lister(),
	}

	controllerRestCfg := rest.CopyConfig(restCfg)
	if restTimeout != nil {
		controllerRestCfg.Timeout = *restTimeout
	}

	cfg := &cfg{
		enabledControllers: enabledControllers,
		restCfg:            controllerRestCfg,
		restTimeout:        restTimeout,

		kubeInformerFactory:    kubeInformerFactory,
		shipperInformerFactory: shipperInformerFactory,
		resync:                 resync,

		recorder: recorder,

		store: store,

		chartVersionResolver: repo.ResolveChartVersionFunc(repoCatalog),
		chartFetcher:         repo.FetchChartFunc(repoCatalog),

		ns:      *ns,
		workers: *workers,

		wg:     wg,
		stopCh: stopCh,

		metrics: &metricsCfg{
			readyCh:      metricsReadyCh,
			wqMetrics:    shippermetrics.NewProvider(),
			restLatency:  shippermetrics.NewRESTLatencyMetric(),
			restResult:   shippermetrics.NewRESTResultMetric(),
			stateMetrics: ssm,
		},
	}

	go func() {
		klog.V(1).Infof("Metrics will listen on %s", *metricsAddr)
		<-metricsReadyCh

		klog.V(3).Info("Starting the metrics web server")
		defer klog.V(3).Info("The metrics web server has shut down")

		runMetrics(cfg.metrics)
	}()

	runControllers(cfg)
}

type klogStdLogger struct{}

func (klogStdLogger) Println(v ...interface{}) {
	// Prometheus only logs errors (which aren't fatal so we downgrade them to
	// warnings).
	klog.Warning(v...)
}

func runMetrics(cfg *metricsCfg) {
	prometheus.MustRegister(cfg.wqMetrics.GetMetrics()...)
	prometheus.MustRegister(cfg.restLatency.Summary, cfg.restResult.Counter)
	prometheus.MustRegister(instrumentedclient.GetMetrics()...)
	prometheus.MustRegister(cfg.stateMetrics)

	srv := http.Server{
		Addr: *metricsAddr,
		Handler: promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{
				ErrorHandling: promhttp.ContinueOnError,
				ErrorLog:      klogStdLogger{},
			},
		),
	}
	err := srv.ListenAndServe()
	if err != nil {
		klog.Fatalf("could not start /metrics endpoint: %s", err)
	}
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
			klog.Fatalf("cannot enable %q: it is not a known controller", controller)
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
			klog.Fatalf("cannot disable %q: it is not a known controller", controller)
		}

		willRun[controller] = false
	}

	return willRun
}

func runControllers(cfg *cfg) {
	controllerInitializers := buildInitializers()

	// This needs to happen before controllers start, so we can start tracking
	// metrics immediately, even before they're exposed to the world.
	workqueue.SetProvider(cfg.metrics.wqMetrics)
	kuberestmetrics.Register(cfg.metrics.restLatency, cfg.metrics.restResult)

	for name, initializer := range controllerInitializers {
		started, err := initializer(cfg)
		// TODO make it visible when some controller's aren't starting properly; all of the initializers return 'nil' ATM
		if err != nil {
			klog.Fatalf("%q failed to initialize", name)
		}

		if !started {
			klog.Infof("%q was skipped per config", name)
		}
	}

	// Controllers and their workqueues have been created, we can expose the
	// metrics now.
	close(cfg.metrics.readyCh)

	go cfg.kubeInformerFactory.Start(cfg.stopCh)
	go cfg.shipperInformerFactory.Start(cfg.stopCh)

	doneCh := make(chan struct{})

	go func() {
		cfg.wg.Wait()
		close(doneCh)
	}()

	<-doneCh
	klog.Info("Controllers have shut down")
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
	controllers["installation"] = startInstallationController
	controllers["capacity"] = startCapacityController
	controllers["traffic"] = startTrafficController
	return controllers
}

func startInstallationController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["installation"]
	if !enabled {
		return false, nil
	}

	dynamicClientBuilderFunc := func(gvk *schema.GroupVersionKind) (dynamic.Interface, error) {
		return client.NewDynamicClient(installation.AgentName, cfg.restCfg, gvk)
	}

	c := installation.NewController(
		client.NewKubeClientOrDie(installation.AgentName, cfg.restCfg),
		cfg.kubeInformerFactory,
		client.NewShipperClientOrDie(installation.AgentName, cfg.restCfg),
		cfg.shipperInformerFactory,
		dynamicClientBuilderFunc,
		cfg.chartFetcher,
		cfg.recorder(installation.AgentName),
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
		client.NewKubeClientOrDie(capacity.AgentName, cfg.restCfg),
		cfg.kubeInformerFactory,
		client.NewShipperClientOrDie(capacity.AgentName, cfg.restCfg),
		cfg.shipperInformerFactory,
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
		client.NewKubeClientOrDie(traffic.AgentName, cfg.restCfg),
		cfg.kubeInformerFactory,
		client.NewShipperClientOrDie(traffic.AgentName, cfg.restCfg),
		cfg.shipperInformerFactory,
		cfg.recorder(traffic.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}
