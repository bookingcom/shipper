package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"encoding/json"
	"io/ioutil"

	"github.com/gorilla/mux"
	"github.com/bookingcom/gopath/src/booking/tell"
	"github.com/bookingcom/shipper/shipping"
)

// AccessTokenHeader is the header name where the Passport access token is expected
const AccessTokenHeader = "X-Access-Token"

func init() {
	flag.Parse()
}

func main() {
	router := mux.NewRouter()
	router.HandleFunc("/api/ship", shipHandler).Methods("POST")
	router.HandleFunc("/", indexHandler)

	server := createServer(router)
	err := server.ListenAndServe()
	if err != nil {
		tell.Fatalln(err)
	}
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Shipped!")
}

func shipHandler(w http.ResponseWriter, r *http.Request) {
	// The access token is mandatory. If not present, return an error right away.
	accessToken := r.Header.Get(AccessTokenHeader)
	if accessToken == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%s header is required", AccessTokenHeader)
		return
	}

	// The application name is mandatory. If not present, return an error right away.
	appName := r.URL.Query().Get("appName")
	if appName == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "application name is required")
	}

	// Extract ShipmentRequest from the body. If an error happens, return it right away.
	var request shipping.ShipmentRequest
	b, _ := ioutil.ReadAll(r.Body)
	if err := json.Unmarshal(b, &request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "couldn't unmarshal the shipment request")
	}

	// Try to Ship the application using the extracted ShipmentRequest on behalf of the given access token. If an
	// error is returned, return it right away.
	err := shipping.Ship(appName, &request, accessToken)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, err)
	}
}

func createServer(h http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":8080",
		Handler:           h,
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}
