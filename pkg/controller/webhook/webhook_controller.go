package webhook

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	"net/http"
)

type Controller struct {
	bindAddr string
	bindPort string

	tlsCertFile       string
	tlsPrivateKeyFile string
}

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
	mux.HandleFunc("/admit", c.admitHandlerFunc)
	mux.HandleFunc("/mutate", c.mutateHandlerFunc)
	return mux
}

func (c *Controller) admitHandlerFunc(writer http.ResponseWriter, request *http.Request) {

}

func (c *Controller) mutateHandlerFunc(writer http.ResponseWriter, request *http.Request) {

}
