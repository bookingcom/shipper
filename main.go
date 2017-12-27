package main

import (
	"log"
	"net/http"

	"github.com/go-ozzo/ozzo-routing"
	"github.com/go-ozzo/ozzo-routing/fault"
)

func main() {
	router := routing.New()
	router.Use(
		fault.Recovery(log.Printf),
	)

	err := http.ListenAndServe(":8080", router)
	if err != nil {
		log.Fatalln(err)
	}
}
