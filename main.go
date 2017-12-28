package main

import (
	"flag"
	"net/http"

	"github.com/go-ozzo/ozzo-routing"
	"github.com/go-ozzo/ozzo-routing/fault"
	"github.com/bookingcom/gopath/src/booking/tell"
)

func init() {
	flag.Parse()
}

func main() {
	router := routing.New()
	router.Use(
		fault.Recovery(tell.Errorf),
	)

	err := http.ListenAndServe(":8080", router)
	if err != nil {
		tell.Fatalln(err)
	}
}
