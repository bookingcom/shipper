package main

import (
	"flag"

	"github.com/bookingcom/shipper/cmd/shipperctl/cmd"
)

func main() {
	flag.Parse()
	cmd.Execute()
}
