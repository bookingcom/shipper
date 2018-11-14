package errors

import (
	"fmt"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ContenderNotFoundError struct {
	appName string
}

func (e ContenderNotFoundError) Error() string {
	return fmt.Sprintf("no contender release found for application %q", e.appName)
}

func IsContenderNotFoundError(err error) bool {
	_, ok := err.(*ContenderNotFoundError)
	return ok
}

func NewContenderNotFoundError(appName string) error {
	return &ContenderNotFoundError{appName: appName}
}

type IncumbentNotFoundError struct {
	appName string
}

func (e IncumbentNotFoundError) Error() string {
	return fmt.Sprintf("no incumbent release found for application %q", e.appName)
}

func IsIncumbentNotFoundError(err error) bool {
	_, ok := err.(*IncumbentNotFoundError)
	return ok
}

func NewIncumbentNotFoundError(appName string) error {
	return &IncumbentNotFoundError{appName: appName}
}

type MissingGenerationAnnotationError struct {
	relName string
}

func (e MissingGenerationAnnotationError) Error() string {
	return fmt.Sprintf("missing label %q in release %q", shipper.ReleaseGenerationAnnotation, e.relName)
}

func IsMissingGenerationAnnotationError(err error) bool {
	_, ok := err.(*MissingGenerationAnnotationError)
	return ok
}

func NewMissingGenerationAnnotationError(relName string) error {
	return &MissingGenerationAnnotationError{relName}
}

type InvalidGenerationAnnotationError struct {
	relName string
	err     error
}

func (e *InvalidGenerationAnnotationError) Error() string {
	return fmt.Sprintf("invalid value for label %q in release %q: %s", shipper.ReleaseGenerationAnnotation, e.relName, e.err)
}

func IsInvalidGenerationAnnotationError(err error) bool {
	_, ok := err.(*InvalidGenerationAnnotationError)
	return ok
}

func NewInvalidGenerationAnnotationError(relName string, err error) error {
	return &InvalidGenerationAnnotationError{relName: relName, err: err}
}
