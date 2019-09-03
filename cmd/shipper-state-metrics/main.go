package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	addr       = flag.String("addr", ":8890", "Addr to expose /metrics on.")
	ns         = flag.String("namespace", shipper.ShipperNamespace, "Namespace for Shipper resources.")

	relDurationBuckets = flag.String("release-duration-buckets", "15,30,45,60,120", "Comma-separated list of buckets for the shipper_objects_release_durations histogram, in seconds")
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

	stopCh := setupSignalHandler()

	resync := time.Second * 0
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

		releaseDurationBuckets: parseFloat64Slice(*relDurationBuckets),
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

func parseFloat64Slice(str string) []float64 {
	strSlice := strings.Split(str, ",")
	float64Slice := make([]float64, len(strSlice))

	for i, b := range strSlice {
		n, err := strconv.ParseFloat(b, 64)
		if err != nil {
			klog.Fatal(err)
		}
		float64Slice[i] = n
	}

	return float64Slice
}

type klogStdLogger struct{}

func (klogStdLogger) Println(v ...interface{}) {
	// Prometheus only logs errors (which aren't fatal so we downgrade them to
	// warnings).
	klog.Warning(v...)
}
