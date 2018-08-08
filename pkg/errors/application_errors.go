package errors

import "fmt"

type ApplicationAnnotationError struct {
	appName        string
	annotationName string
	err            error
}

func (e *ApplicationAnnotationError) Error() string {
	return fmt.Sprintf(`error decoding annotation %q in application %q: %s`, e.annotationName, e.appName, e.err)
}

func NewApplicationAnnotationError(appName, annotationName string, err error) error {
	return &ApplicationAnnotationError{appName: appName, annotationName: annotationName, err: err}
}

func IsApplicationAnnotationNotFoundError(err error) bool {
	_, ok := err.(*ApplicationAnnotationError)
	return ok
}
