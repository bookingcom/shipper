package errors

import (
	"errors"
	"testing"
)

func makeRetriable() error {
	return NewRecoverableError(errors.New("recoverable"))
}

func makeUnretriable() error {
	return NewUnrecoverableError(errors.New("unrecoverable"))
}

func TestShouldRetry(t *testing.T) {
	errors := []struct {
		err      error
		expected bool
	}{
		{errors.New("generic error"), true},
		{makeRetriable(), true},
		{makeUnretriable(), false},
	}

	for _, tt := range errors {
		shouldRetry := ShouldRetry(tt.err)
		if shouldRetry != tt.expected {
			t.Errorf("expected error %T to be retriable %t, got retriable %t", tt.err, tt.expected, shouldRetry)
		}
	}
}

func TestMultiError_ShouldRetry(t *testing.T) {
	retriableMulti := NewMultiError()
	retriableMulti.Append(makeRetriable())
	retriableMulti.Append(makeUnretriable())
	if !retriableMulti.ShouldRetry() {
		t.Error("expected multierror with retriable errors to be retriable")
	}

	unretriableMulti := NewMultiError()
	unretriableMulti.Append(makeUnretriable())
	if unretriableMulti.ShouldRetry() {
		t.Error("expected multierror without any retriable errors to be non-retriable")
	}
}
