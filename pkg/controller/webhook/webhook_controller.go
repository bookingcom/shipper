package webhook

import (
	"context"
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
	return &Controller{}
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	addr := c.bindAddr + ":" + c.bindPort
	mux := c.initializeHandlers()
	server := &http.Server{Addr: addr, Handler: mux}

	var serverError error = nil
	if c.tlsCertFile == "" || c.tlsPrivateKeyFile == "" {
		serverError = server.ListenAndServe()
	} else {
		serverError = server.ListenAndServeTLS(c.tlsCertFile, c.tlsPrivateKeyFile)
	}

	if serverError != nil {
		glog.Fatalf("failed to start shipper-webhook-controller: %v", serverError)
	}

	glog.V(4).Info("Started WebHook controller")

	<-stopCh

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
