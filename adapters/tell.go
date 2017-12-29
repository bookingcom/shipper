package adapters

import (
	"github.com/bookingcom/gopath/src/booking/tell"
)

type Tell struct{}

func (t *Tell) Println(a ...interface{}) {
	tell.Errorln(a...)
}
