package errors

import (
	"fmt"
	"sort"
	"strings"
)

const groupErrorDisplayLimit = 3

type SelfAwareError interface {
	error
	Name() string
	ShouldRetry() bool
}

func Classify(err error) SelfAwareError {
	selfAware, ok := err.(SelfAwareError)
	if !ok {
		return NewUnknown(err)
	}
	return selfAware
}

type Group struct {
	errors []SelfAwareError
}

func NewGroup() *Group {
	return &Group{}
}

func (g *Group) Collect(err SelfAwareError) {
	g.errors = append(g.errors, err)
}

func (g *Group) Len() int {
	return len(g.errors)
}

func (g *Group) Name() string {
	return "MultipleErrors"
}

func (g *Group) Error() string {
	errorsByName := map[string][]SelfAwareError{}
	for _, err := range g.errors {
		_, ok := errorsByName[err.Name()]
		if !ok {
			errorsByName[err.Name()] = []SelfAwareError{}
		}
		errorsByName[err.Name()] = append(errorsByName[err.Name()], err)
	}

	sort.Slice(g.errors, func(i, j int) bool {
		return len(errorsByName[g.errors[i].Name()]) > len(errorsByName[g.errors[j].Name()])
	})

	displayLimit := groupErrorDisplayLimit
	if len(g.errors) < groupErrorDisplayLimit {
		displayLimit = len(g.errors)
	}

	display := make([]string, 0, displayLimit)
	// NOTE(btyler) this is not a comprehensive display: it just picks the
	// first error of a given type to display the message.
	seen := map[string]bool{}
	for _, err := range g.errors {
		_, ok := seen[err.Name()]
		if !ok {
			display = append(
				display,
				fmt.Sprintf("%d: %s x%d: %s", len(display)+1, err.Name(), len(errorsByName[err.Name()]), err),
			)
			if len(display) >= displayLimit {
				break
			}
			seen[err.Name()] = true
		}
	}
	return strings.Join(display, "\n")
}

// in the interest of doing as much work as possible, if any of the errors we
// collect can be retried, let's retry
func (g *Group) ShouldRetry() bool {
	for _, err := range g.errors {
		if err.ShouldRetry() {
			return true
		}
	}
	return false
}

type Unknown struct {
	err error
}

func IsUnknown(err error) bool {
	_, ok := err.(Unknown)
	return ok
}

func (e Unknown) Name() string {
	return "Unknown"
}

func (e Unknown) Error() string {
	return fmt.Sprintf(
		"An unknown error occurred: %q", e.err,
	)
}

func (e Unknown) ShouldRetry() bool {
	return true
}

func NewUnknown(err error) Unknown {
	return Unknown{
		err: err,
	}
}
