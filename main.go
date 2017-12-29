package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/bookingcom/gopath/src/booking/tell"
	"github.com/bookingcom/shipper/adapters"
)

func init() {
	flag.Parse()
}

func main() {
	router := mux.NewRouter()
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

func createServer(h http.Handler) *http.Server {
	tell := &adapters.Tell{}
	recoveryHandler := handlers.RecoveryHandler(handlers.RecoveryLogger(tell), handlers.PrintRecoveryStack(true))

	return &http.Server{
		Addr:              ":8080",
		Handler:           recoveryHandler(h),
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}
