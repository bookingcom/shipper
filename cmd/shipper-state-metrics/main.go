package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
)

var (
	masterURL    = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig   = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	resyncPeriod = flag.String("resync", "5m", "Informer's cache re-sync in Go's duration format.")
	addr         = flag.String("addr", ":8890", "Addr to expose /metrics on.")
	ns           = flag.String("namespace", shipper.ShipperNamespace, "Namespace for Shipper resources.")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	klog.Infof("Starting shipper-state-metrics on %s", *addr)
	defer klog.Info("Stopping shipper-state-metrics")

	restCfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		klog.Fatal(err)
	}

	shipperClient, err := shipperclientset.NewForConfig(restCfg)
	if err != nil {
		klog.Fatal(err)
	}

	kubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		klog.Fatal(err)
	}

	resync, err := time.ParseDuration(*resyncPeriod)
	if err != nil {
		klog.Warningf("Couldn't parse resync period %q, defaulting to 5 minutes", *resyncPeriod)
		resync = 5 * time.Minute
	}

	stopCh := setupSignalHandler()

	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperClient, resync)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, resync)

	ssm := ShipperStateMetrics{
		appsLister:     shipperInformerFactory.Shipper().V1alpha1().Applications().Lister(),
		relsLister:     shipperInformerFactory.Shipper().V1alpha1().Releases().Lister(),
		itsLister:      shipperInformerFactory.Shipper().V1alpha1().InstallationTargets().Lister(),
		ctsLister:      shipperInformerFactory.Shipper().V1alpha1().CapacityTargets().Lister(),
		ttsLister:      shipperInformerFactory.Shipper().V1alpha1().TrafficTargets().Lister(),
		clustersLister: shipperInformerFactory.Shipper().V1alpha1().Clusters().Lister(),
		rbLister:       shipperInformerFactory.Shipper().V1alpha1().RolloutBlocks().Lister(),

		nssLister:     kubeInformerFactory.Core().V1().Namespaces().Lister(),
		secretsLister: kubeInformerFactory.Core().V1().Secrets().Lister(),

		shipperNs: *ns,
	}
	prometheus.MustRegister(ssm)

	shipperInformerFactory.Start(stopCh)
	kubeInformerFactory.Start(stopCh)

	shipperInformerFactory.WaitForCacheSync(stopCh)
	kubeInformerFactory.WaitForCacheSync(stopCh)

	go func() {
		srv := http.Server{
			Addr: *addr,
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
	}()

	<-stopCh
}

func setupSignalHandler() <-chan struct{} {
	stopCh := make(chan struct{})

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		close(stopCh)
		<-sigCh
		os.Exit(1) // Second signal. Exit directly.
	}()

	return stopCh
}

type klogStdLogger struct{}

func (klogStdLogger) Println(v ...interface{}) {
	// Prometheus only logs errors (which aren't fatal so we downgrade them to
	// warnings).
	klog.Warning(v...)
}
