package controller

import "fmt"

type MultipleOwnerReferencesError error

func NewMultipleOwnerReferencesError(name string, references int) MultipleOwnerReferencesError {
	return MultipleOwnerReferencesError(fmt.Errorf(
		"expected exactly one owner for object %q, got %d",
		name, references))
}
