package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/bookingcom/gopath/src/booking/tell"
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
	recoveryHandler := handlers.RecoveryHandler()
	return &http.Server{
		Addr:              ":8080",
		Handler:           recoveryHandler(h),
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
}
