package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/golang/glog"

	admission_v1beta1 "k8s.io/api/admission/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	rolloutblockUtil "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	kubeclient "k8s.io/api/admission/v1beta1"
)

type Webhook struct {
	shipperClientset clientset.Interface
	bindAddr string
	bindPort string

	tlsCertFile       string
	tlsPrivateKeyFile string
}

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func NewWebhook(bindAddr, bindPort, tlsPrivateKeyFile, tlsCertFile string, shipperClientset clientset.Interface) *Webhook {
	return &Webhook{
		shipperClientset:  shipperClientset,
		bindAddr:          bindAddr,
		bindPort:          bindPort,
		tlsPrivateKeyFile: tlsPrivateKeyFile,
		tlsCertFile:       tlsCertFile,
	}
}

func (c *Webhook) Run(stopCh <-chan struct{}) {
	addr := c.bindAddr + ":" + c.bindPort
	mux := c.initializeHandlers()
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		var serverError error
		if c.tlsCertFile == "" || c.tlsPrivateKeyFile == "" {
			serverError = server.ListenAndServe()
		} else {
			serverError = server.ListenAndServeTLS(c.tlsCertFile, c.tlsPrivateKeyFile)
		}

		if serverError != nil && serverError != http.ErrServerClosed {
			glog.Fatalf("failed to start shipper-webhook: %v", serverError)
		}
	}()

	glog.V(2).Info("Started the WebHook")

	<-stopCh

	glog.V(2).Info("Shutting down the WebHook")

	if err := server.Shutdown(context.Background()); err != nil {
		glog.Errorf(`HTTP server Shutdown: %v`, err)
	}
}

func (c *Webhook) initializeHandlers() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/validate", adaptHandler(c.validateHandlerFunc))
	return mux
}

// adaptHandler wraps an admission review function to be consumed through HTTP.
func adaptHandler(handler func(*admission_v1beta1.AdmissionReview) *admission_v1beta1.AdmissionResponse) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			if data, err := ioutil.ReadAll(r.Body); err == nil {
				body = data
			}
		}

		if len(body) == 0 {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}

		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, "Invalid content-type", http.StatusUnsupportedMediaType)
			return
		}

		if mediaType != "application/json" {
			http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
			return
		}

		var admissionResponse *admission_v1beta1.AdmissionResponse
		ar := admission_v1beta1.AdmissionReview{}
		if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
			admissionResponse = &admission_v1beta1.AdmissionResponse{
				Result: &meta_v1.Status{
					Message: err.Error(),
				},
			}
		} else {
			admissionResponse = handler(&ar)
		}

		admissionReview := admission_v1beta1.AdmissionReview{}
		if admissionResponse != nil {
			admissionReview.Response = admissionResponse
			if ar.Request != nil {
				admissionReview.Response.UID = ar.Request.UID
			}
		}

		resp, err := json.Marshal(admissionReview)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(resp); err != nil {
			http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
			return
		}
	}
}

func (c *Webhook) validateHandlerFunc(review *admission_v1beta1.AdmissionReview) *admission_v1beta1.AdmissionResponse {
	request := review.Request
	var err error

	switch request.Kind.Kind {
	case "Application":
		var application shipper.Application
		err = json.Unmarshal(request.Object.Raw, &application)
		if err == nil {
			err = c.validateApplication(request, application)
		}
	case "Release":
		var release shipper.Release
		err = json.Unmarshal(request.Object.Raw, &release)
		if err == nil {
			err = c.validateRelease(request, release)
		}
	case "Cluster":
		var cluster shipper.Cluster
		err = json.Unmarshal(request.Object.Raw, &cluster)
	case "InstallationTarget":
		var installationTarget shipper.InstallationTarget
		err = json.Unmarshal(request.Object.Raw, &installationTarget)
	case "CapacityTarget":
		var capacityTarget shipper.CapacityTarget
		err = json.Unmarshal(request.Object.Raw, &capacityTarget)
	case "TrafficTarget":
		var trafficTarget shipper.TrafficTarget
		err = json.Unmarshal(request.Object.Raw, &trafficTarget)
	case "RolloutBlock":
		var rolloutBlock shipper.RolloutBlock
		err = json.Unmarshal(request.Object.Raw, &rolloutBlock)
	}

	if request.Operation == kubeclient.Delete {
		err = nil
	}

	if err != nil {
		return &admission_v1beta1.AdmissionResponse{
			Result: &meta_v1.Status{
				Message: err.Error(),
			},
		}
	}

	return &admission_v1beta1.AdmissionResponse{
		Allowed: true,
	}
}

func (c *Webhook) validateRelease(request *admission_v1beta1.AdmissionRequest, release shipper.Release) error {
	var err error
	if request.Operation == kubeclient.Create {
		err = c.shouldBlockRelease(release)
	} else if request.Operation == kubeclient.Update {
		overrideRB, ok := release.Annotations[shipper.RolloutBlocksOverrideAnnotation]
		if ok {
			err = c.validateOverrideRolloutBlockAnnotation(overrideRB, release.Namespace)
		}
	}
	return err
}

func (c *Webhook) validateApplication(request *admission_v1beta1.AdmissionRequest, application shipper.Application) error {
	var err error
	if request.Operation == kubeclient.Create {
		err = c.shouldBlockApplication(application)
	} else if request.Operation == kubeclient.Update {
		overrideRB, ok := application.Annotations[shipper.RolloutBlocksOverrideAnnotation]
		if ok {
			err = c.validateOverrideRolloutBlockAnnotation(overrideRB, application.Namespace)
		}
	}
	return err
}

func (c *Webhook) shouldBlockApplication(app shipper.Application) error {
	ns := app.Namespace
	overrideRB, ok := app.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
	}

	return c.shouldBlockRollout(ns, overrideRB)
}

func (c *Webhook) shouldBlockRelease(release shipper.Release) error {
	ns := release.Namespace
	overrideRB, ok := release.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
	}

	return c.shouldBlockRollout(ns, overrideRB)
}

func (c *Webhook) shouldBlockRollout(namespace string, overrideRB string) error {
	var (
		err          error
		nsRBs, gbRBs []*shipper.RolloutBlock
	)

	nsRBs, gbRBs = c.existingRolloutBlocks(namespace)

	if err = c.validateOverrideRolloutBlockAnnotation(overrideRB, namespace); err != nil {
		return err
	}

	overrideRolloutBlock, eventMessage, err := rolloutblockUtil.ShouldOverrideRolloutBlock(overrideRB, nsRBs, gbRBs)
	if err != nil {
		return err
	}

	if overrideRolloutBlock {
		return nil
	}

	return shippererrors.NewRolloutBlockError(eventMessage)
}

func (c *Webhook) existingRolloutBlocks(namespace string) ([]*shipper.RolloutBlock, []*shipper.RolloutBlock) {
	var nsRBs, gbRBs []*shipper.RolloutBlock
	if nsRBList, err := c.shipperClientset.ShipperV1alpha1().RolloutBlocks(namespace).List(meta_v1.ListOptions{}); err == nil {
		for _, item := range nsRBList.Items {
			nsRBs = append(nsRBs, &item)
		}
	}
	if gbRBList, err := c.shipperClientset.ShipperV1alpha1().RolloutBlocks(shipper.ShipperNamespace).List(meta_v1.ListOptions{}); err == nil {
		for _, item := range gbRBList.Items {
			gbRBs = append(gbRBs, &item)
		}
	}
	return nsRBs, gbRBs
}

func (c *Webhook) validateOverrideRolloutBlockAnnotation(overrideRB string, namespace string) error {
	if len(overrideRB) == 0 {
		return nil
	}

	re, err := regexp.Compile("^[a-zA-Z0-9/-]+/[a-zA-Z0-9/-]+$")
	if err != nil {
		return err
	}

	overrideRbs := strings.Split(overrideRB, ",")
	for _, item := range overrideRbs {
		if !re.MatchString(item) {
			return shippererrors.NewInvalidRolloutBlockOverrideError(item)
		}
	}

	nsRBs, gbRBs := c.existingRolloutBlocks(namespace)
	rbs := append(nsRBs, gbRBs...)
	_, err = rolloutblockUtil.Difference(rbs, overrideRbs)

	return err
}
