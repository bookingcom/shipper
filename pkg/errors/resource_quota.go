package errors

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
)

type ResourceQuotaError struct {
	hard resource.Quantity
	used resource.Quantity
}

func NewResourceQuotaError(hard, used resource.Quantity) ResourceQuotaError {
	return ResourceQuotaError{hard: hard, used: used}
}

func (e ResourceQuotaError) Error() string {
	msg := `resource quota out of bound: limit: %s, used: %s`
	return fmt.Sprintf(msg, e.hard.String(), e.used.String())
}

func (e ResourceQuotaError) ShouldRetry() bool {
	return false
}

