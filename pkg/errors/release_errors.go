package errors

import "fmt"

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
