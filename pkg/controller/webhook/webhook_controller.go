package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"

	admission_v1beta1 "k8s.io/api/admission/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	shipper_v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type Controller struct {
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

func NewController() *Controller {
	return &Controller{
		bindAddr: "0.0.0.0",
		// bindPort must be 443 even if not serving TLS.
		bindPort: "9443",
	}
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	addr := c.bindAddr + ":" + c.bindPort
	mux := c.initializeHandlers()
	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		var serverError error = nil
		if c.tlsCertFile == "" || c.tlsPrivateKeyFile == "" {
			serverError = server.ListenAndServe()
		} else {
			serverError = server.ListenAndServeTLS(c.tlsCertFile, c.tlsPrivateKeyFile)
		}

		if serverError != nil {
			glog.Fatalf("failed to start shipper-webhook-controller: %v", serverError)
		}
	}()

	glog.V(2).Info("Started WebHook controller")

	<-stopCh

	glog.V(2).Info("Shutting down WebHook controller")

	if err := server.Shutdown(context.Background()); err != nil {
		glog.Errorf(`HTTP server Shutdown: %v`, err)
	}
}

func (c *Controller) initializeHandlers() *http.ServeMux {
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

		contentType := r.Header.Get("Content-Type")
		if contentType != "applicaton/json" {
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
		}

		if _, err := w.Write(resp); err != nil {
			http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
		}
	}
}

func (c *Controller) validateHandlerFunc(review *admission_v1beta1.AdmissionReview) *admission_v1beta1.AdmissionResponse {
	request := review.Request
	var err error

	switch request.Kind.Kind {
	case "Application":
		var application shipper_v1alpha1.Application
		err = json.Unmarshal(request.Object.Raw, &application)
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
