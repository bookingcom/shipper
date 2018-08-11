package errors

import (
	"fmt"
	"testing"
)

func TestGroup(t *testing.T) {
	g := NewGroup()

	errors := []SelfAwareError{
		NewFailedAPICall("LIST", "pods", fmt.Errorf("tcp timeout")),
		NewFailedAPICall("LIST", "pods", fmt.Errorf("400: invalid certificate")),
		NewFailedAPICall("LIST", "pods", fmt.Errorf("500: server is sad")),
		NewFailedAPICall("LIST", "pods", fmt.Errorf("500: server is sad")),
		NewNoRegionsSpecified(),
	}

	for _, err := range errors {
		g.Collect(err)
	}

	if !g.ShouldRetry() {
		t.Errorf("expected to retry a group with at least one retryable error, but ShouldRetry was false")
	}

	msg := g.Error()
	expected := fmt.Sprintf(
		"1: %s x4: %s\n2: %s x1: %s",
		errors[0].Name(), errors[0].Error(),
		errors[4].Name(), errors[4].Error(),
	)
	if msg != expected {
		t.Errorf("expected %q for an error message, got %q", expected, msg)
	}
}
