package ui

import (
	"bytes"
	"io"
	"reflect"
	"testing"
)

func TestAskForConfirmation(t *testing.T) {
	tests := []struct {
		Name            string
		stdin           io.Reader
		message         string
		ExpectedConfirm bool
		ExpectedErr     error
	}{
		{
			Name:            "confirm with y",
			stdin:           bytes.NewBufferString("y\n"),
			message:         "",
			ExpectedConfirm: true,
			ExpectedErr:     nil,
		},
		{
			Name:            "confirm with Y",
			stdin:           bytes.NewBufferString("Y\n"),
			message:         "",
			ExpectedConfirm: true,
			ExpectedErr:     nil,
		},
		{
			Name:            "reject with n",
			stdin:           bytes.NewBufferString("n\n"),
			message:         "",
			ExpectedConfirm: false,
			ExpectedErr:     nil,
		},
		{
			Name:            "reject with N",
			stdin:           bytes.NewBufferString("N\n"),
			message:         "",
			ExpectedConfirm: false,
			ExpectedErr:     nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			actualConfirm, actualErr := AskForConfirmation(test.stdin, test.message)
			if !reflect.DeepEqual(test.ExpectedErr, actualErr) {
				t.Fatalf(
					"expected error %q got %q",
					test.ExpectedErr,
					actualErr,
				)
			}
			if !reflect.DeepEqual(test.ExpectedConfirm, actualConfirm) {
				t.Fatalf(
					"expected confirmation %t got %t",
					test.ExpectedConfirm,
					actualConfirm,
				)
			}
		})
	}
}
