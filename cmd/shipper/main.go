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

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
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
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller/application"
	"github.com/bookingcom/shipper/pkg/controller/capacity"
	"github.com/bookingcom/shipper/pkg/controller/clustersecret"
	"github.com/bookingcom/shipper/pkg/controller/installation"
	"github.com/bookingcom/shipper/pkg/controller/janitor"
	"github.com/bookingcom/shipper/pkg/controller/release"
	"github.com/bookingcom/shipper/pkg/controller/rolloutblock"
	"github.com/bookingcom/shipper/pkg/controller/traffic"
	"github.com/bookingcom/shipper/pkg/metrics/instrumentedclient"
	shippermetrics "github.com/bookingcom/shipper/pkg/metrics/prometheus"
	"github.com/bookingcom/shipper/pkg/webhook"
)

var controllers = []string{
	"application",
	"clustersecret",
	"release",
	"installation",
	"capacity",
	"traffic",
	"rolloutblock",
	"janitor",
	"webhook",
}

const defaultRESTTimeout time.Duration = 10 * time.Second
const defaultResync time.Duration = 30 * time.Second

var (
	masterURL           = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig          = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	certPath            = flag.String("cert", "", "Path to the TLS certificate for target clusters.")
	keyPath             = flag.String("key", "", "Path to the TLS private key for target clusters.")
	ns                  = flag.String("namespace", shipper.ShipperNamespace, "Namespace for Shipper resources.")
	enabledControllers  = flag.String("enable", strings.Join(controllers, ","), "comma-seperated list of controllers to run (if not all)")
	disabledControllers = flag.String("disable", "", "comma-seperated list of controllers to disable")
	workers             = flag.Int("workers", 2, "Number of workers to start for each controller.")
	metricsAddr         = flag.String("metrics-addr", ":8889", "Addr to expose /metrics on.")
	chartCacheDir       = flag.String("cachedir", filepath.Join(os.TempDir(), "chart-cache"), "location for the local cache of downloaded charts")
	resync              = flag.Duration("resync", defaultResync, "Informer's cache re-sync in Go's duration format.")
	restTimeout         = flag.Duration("rest-timeout", defaultRESTTimeout, "Timeout value for management and target REST clients. Does not affect informer watches.")
	webhookCertPath     = flag.String("webhook-cert", "", "Path to the TLS certificate for the webhook controller.")
	webhookKeyPath      = flag.String("webhook-key", "", "Path to the TLS private key for the webhook controller.")
	webhookBindAddr     = flag.String("webhook-addr", "0.0.0.0", "Addr to bind the webhook controller.")
	webhookBindPort     = flag.String("webhook-port", "9443", "Port to bind the webhook controller.")
)

type metricsCfg struct {
	readyCh chan struct{}

	wqMetrics   *shippermetrics.PrometheusWorkqueueProvider
	restLatency *shippermetrics.RESTLatencyMetric
	restResult  *shippermetrics.RESTResultMetric
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

	webhookCertPath, webhookKeyPath  string
	webhookBindAddr, webhookBindPort string

	wg     *sync.WaitGroup
	stopCh <-chan struct{}

	metrics *metricsCfg
}

func main() {
	flag.Parse()

	// As we use runtime.HandlerError a lot, and it uses klog instead of
	// glog, we need to sync glog flags into klog, because klog doesn't do
	// anything in init(), as it's the main reason of its fork from glog.
	// NOTE(jgreff): this is also probably a good opportunity to discuss
	// whether we should move shipper to klog entirely.
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			_ = f2.Value.Set(f1.Value.String())
		}
	})

	baseRestCfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		glog.Fatal(err)
	}

	// These are only used in shared informers. Setting HTTP timeout here would
	// affect watches which is undesirable. Instead, we leave it to client-go (see
	// k8s.io/client-go/tools/cache) to govern watch durations.
	informerKubeClient := buildKubeClient(baseRestCfg, "kube-shared-informer", nil)
	informerShipperClient := buildShipperClient(baseRestCfg, "shipper-shared-informer", nil)

	stopCh := setupSignalHandler()
	metricsReadyCh := make(chan struct{})

	kubeInformerFactory := informers.NewSharedInformerFactory(informerKubeClient, *resync)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(informerShipperClient, *resync)

	shipperscheme.AddToScheme(scheme.Scheme)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	func() {
		kubeClient := buildKubeClient(baseRestCfg, "event-broadcaster", restTimeout)
		broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	}()

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
		func(clusterName string, ua string, config *rest.Config) (kubernetes.Interface, error) {
			glog.V(8).Infof("Building a client for Cluster %q, UserAgent %q", clusterName, ua)

			// NOTE(btyler/asurikov) Ooookaaayyy. This is temporary. I promise.
			// No, really. This is to buy us time to think how the
			// clusterclientstore API needs to change to make it nice and easy
			// keeping distinct clients per controller per cluster. The number
			// 30 is 10x the default for each controller that shares a client
			// in the current implementation. This is deliberately high so that
			// we can track optimization efforts with precision; we're
			// reasonably confident that the API server can tolerate it.
			shallowCopy := *config
			shallowCopy.QPS = rest.DefaultQPS * 30
			shallowCopy.Burst = rest.DefaultBurst * 30
			rest.AddUserAgent(&shallowCopy, ua)

			return kubernetes.NewForConfig(&shallowCopy)
		},
		kubeInformerFactory.Core().V1().Secrets(),
		shipperInformerFactory.Shipper().V1alpha1().Clusters(),
		*ns,
		restTimeout,
		resync,
	)

	wg.Add(1)
	go func() {
		store.Run(stopCh)
		wg.Done()
	}()

	glog.V(1).Infof("Chart cache stored at %q", *chartCacheDir)
	glog.V(1).Infof("REST client timeout is %s", *restTimeout)

	repoCatalog := repo.NewCatalog(
		repo.DefaultFileCacheFactory(*chartCacheDir),
		repo.DefaultRemoteFetcher,
	)

	cfg := &cfg{
		enabledControllers: enabledControllers,
		restCfg:            baseRestCfg,
		restTimeout:        restTimeout,

		kubeInformerFactory:    kubeInformerFactory,
		shipperInformerFactory: shipperInformerFactory,
		resync:                 resync,

		recorder: recorder,

		store: store,

		chartVersionResolver: repo.ResolveChartVersionFunc(repoCatalog),
		chartFetcher:         repo.FetchChartFunc(repoCatalog),

		certPath: *certPath,
		keyPath:  *keyPath,
		ns:       *ns,
		workers:  *workers,

		webhookCertPath: *webhookCertPath,
		webhookKeyPath:  *webhookKeyPath,
		webhookBindAddr: *webhookBindAddr,
		webhookBindPort: *webhookBindPort,

		wg:     wg,
		stopCh: stopCh,

		metrics: &metricsCfg{
			readyCh:     metricsReadyCh,
			wqMetrics:   shippermetrics.NewProvider(),
			restLatency: shippermetrics.NewRESTLatencyMetric(),
			restResult:  shippermetrics.NewRESTResultMetric(),
		},
	}

	go func() {
		glog.V(1).Infof("Metrics will listen on %s", *metricsAddr)
		<-metricsReadyCh

		glog.V(3).Info("Starting the metrics web server")
		defer glog.V(3).Info("The metrics web server has shut down")

		runMetrics(cfg.metrics)
	}()

	runControllers(cfg)
}

type glogStdLogger struct{}

func (glogStdLogger) Println(v ...interface{}) {
	// Prometheus only logs errors (which aren't fatal so we downgrade them to
	// warnings).
	glog.Warning(v...)
}

func runMetrics(cfg *metricsCfg) {
	prometheus.MustRegister(cfg.wqMetrics.GetMetrics()...)
	prometheus.MustRegister(cfg.restLatency.Summary, cfg.restResult.Counter)
	prometheus.MustRegister(instrumentedclient.GetMetrics()...)

	srv := http.Server{
		Addr: *metricsAddr,
		Handler: promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{
				ErrorHandling: promhttp.ContinueOnError,
				ErrorLog:      glogStdLogger{},
			},
		),
	}
	err := srv.ListenAndServe()
	if err != nil {
		glog.Fatalf("could not start /metrics endpoint: %s", err)
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

	// This needs to happen before controllers start, so we can start tracking
	// metrics immediately, even before they're exposed to the world.
	workqueue.SetProvider(cfg.metrics.wqMetrics)
	kuberestmetrics.Register(cfg.metrics.restLatency, cfg.metrics.restResult)

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
	controllers["application"] = startApplicationController
	controllers["clustersecret"] = startClusterSecretController
	controllers["release"] = startReleaseController
	controllers["installation"] = startInstallationController
	controllers["capacity"] = startCapacityController
	controllers["traffic"] = startTrafficController
	controllers["rolloutblock"] = startRolloutBlockController
	controllers["janitor"] = startJanitorController
	controllers["webhook"] = startWebhook
	return controllers
}

func startApplicationController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["application"]
	if !enabled {
		return false, nil
	}

	c := application.NewController(
		buildShipperClient(cfg.restCfg, application.AgentName, cfg.restTimeout),
		cfg.shipperInformerFactory,
		cfg.chartVersionResolver,
		cfg.recorder(application.AgentName),
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
		buildKubeClient(cfg.restCfg, clustersecret.AgentName, cfg.restTimeout),
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

func startReleaseController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["release"]
	if !enabled {
		return false, nil
	}

	c := release.NewController(
		buildShipperClient(cfg.restCfg, release.AgentName, cfg.restTimeout),
		cfg.shipperInformerFactory,
		cfg.chartFetcher,
		cfg.recorder(release.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startInstallationController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["installation"]
	if !enabled {
		return false, nil
	}

	dynamicClientBuilderFunc := func(gvk *schema.GroupVersionKind, config *rest.Config, cluster *shipper.Cluster) dynamic.Interface {
		config.APIPath = dynamic.LegacyAPIPathResolverFunc(*gvk)
		config.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}

		if cfg.restTimeout != nil {
			config.Timeout = *cfg.restTimeout
		}

		dynamicClient, newClientErr := dynamic.NewForConfig(config)
		if newClientErr != nil {
			glog.Fatal(newClientErr)
		}
		return dynamicClient
	}

	c := installation.NewController(
		buildShipperClient(cfg.restCfg, installation.AgentName, cfg.restTimeout),
		cfg.shipperInformerFactory,
		cfg.store,
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
		buildShipperClient(cfg.restCfg, capacity.AgentName, cfg.restTimeout),
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
		buildShipperClient(cfg.restCfg, traffic.AgentName, cfg.restTimeout),
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

func startRolloutBlockController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["rolloutblock"]
	if !enabled {
		return false, nil
	}

	c := rolloutblock.NewController(
		buildShipperClient(cfg.restCfg, rolloutblock.AgentName, cfg.restTimeout),
		cfg.shipperInformerFactory,
		cfg.recorder(rolloutblock.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startWebhook(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["webhook"]
	if !enabled {
		return false, nil
	}

	c := webhook.NewWebhook(
		cfg.webhookBindAddr,
		cfg.webhookBindPort,
		cfg.webhookKeyPath,
		cfg.webhookCertPath,
		buildShipperClient(cfg.restCfg, rolloutblock.AgentName, cfg.restTimeout))

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func startJanitorController(cfg *cfg) (bool, error) {
	enabled := cfg.enabledControllers["janitor"]
	if !enabled {
		return false, nil
	}

	c := janitor.NewController(
		buildShipperClient(cfg.restCfg, janitor.AgentName, cfg.restTimeout),
		cfg.shipperInformerFactory,
		cfg.store,
		cfg.recorder(janitor.AgentName),
	)

	cfg.wg.Add(1)
	go func() {
		c.Run(cfg.workers, cfg.stopCh)
		cfg.wg.Done()
	}()

	return true, nil
}

func buildShipperClient(restCfg *rest.Config, ua string, timeout *time.Duration) *shipperclientset.Clientset {
	shallowCopy := *restCfg

	rest.AddUserAgent(&shallowCopy, ua)

	if timeout != nil {
		shallowCopy.Timeout = *timeout
	}

	// NOTE(btyler): These are deliberately high: we're reasonably certain the
	// API servers can handle a much larger number of requests, and we want to
	// have better sensitivity to any shifts in API call efficiency (as well as
	// give users a better experience by reducing queue latency). I plan to
	// turn this back down once we've got some metrics on where our current ratio
	// of shipper objects to API calls is and we start working towards optimizing
	// that ratio.
	shallowCopy.QPS = rest.DefaultQPS * 10
	shallowCopy.Burst = rest.DefaultBurst * 10

	return shipperclientset.NewForConfigOrDie(&shallowCopy)
}

func buildKubeClient(restCfg *rest.Config, ua string, timeout *time.Duration) *kubernetes.Clientset {
	shallowCopy := *restCfg

	rest.AddUserAgent(&shallowCopy, ua)

	if timeout != nil {
		shallowCopy.Timeout = *timeout
	}
	// NOTE(btyler): Like with the Shipper client, these are deliberately high.
	// The vast majority of API calls here are Events, so optimization in
	// this case will be examining utility of the various events we emit.

	shallowCopy.QPS = rest.DefaultQPS * 10
	shallowCopy.Burst = rest.DefaultBurst * 10

	return kubernetes.NewForConfigOrDie(&shallowCopy)
}
