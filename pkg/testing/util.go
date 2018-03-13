package testing

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/diff"
	kubetesting "k8s.io/client-go/testing"
)

const TestNamespace = "test-namespace"

// CheckActions takes a slice of expected actions and a slice of observed
// actions (typically obtained from fakeClient.Actions()) and compares them.
// Calls Errorf on t for every difference it finds.
func CheckActions(expected, actual []kubetesting.Action, t *testing.T) {
	for i, action := range actual {
		if len(expected) < i+1 {
			t.Errorf("%d unexpected actions: %+v", len(actual)-len(expected), actual[i:])
			break
		}

		CheckAction(expected[i], action, t)
	}

	if len(expected) > len(actual) {
		t.Errorf("missing %d expected actions: %+v", len(expected)-len(actual), expected[len(actual):])
	}
}

// CheckAction compares two individual actions and calls Errorf on t if it finds
// a difference.
func CheckAction(expected, actual kubetesting.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) &&
		actual.GetSubresource() == expected.GetSubresource() &&
		actual.GetResource() == expected.GetResource()) {

		t.Errorf("expected\n\t%#v\ngot\n\t%#v", expected, actual)
		return
	}

	if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
		t.Errorf("expected action %T but got %T", expected, actual)
		return
	}

	switch a := actual.(type) {

	case kubetesting.CreateAction:
		e, _ := expected.(kubetesting.CreateAction)
		expObject := e.GetObject()
		object := a.GetObject()

		if expObject != nil && !reflect.DeepEqual(expObject, object) {
			t.Errorf("Action %s %s has wrong object\nDiff:\n %s",
				a.GetResource().Resource, a.GetVerb(), diff.ObjectGoPrintDiff(expObject, object))
		}

	case kubetesting.UpdateAction:
		e, _ := expected.(kubetesting.UpdateAction)
		expObject := e.GetObject()
		object := a.GetObject()

		if expObject != nil && !reflect.DeepEqual(expObject, object) {
			t.Errorf("Action %s %s has wrong object\nDiff:\n %s",
				a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintDiff(expObject, object))
		}

	case kubetesting.PatchAction:
		e, _ := expected.(kubetesting.PatchAction)
		expObject := string(e.GetPatch())
		object := string(a.GetPatch())

		if !reflect.DeepEqual(expObject, object) {
			t.Errorf("Action %s %s has wrong object\nDiff:\n %s",
				a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintDiff(expObject, object))
		}
	}
}

// FilterActions, given a slice of observed actions, returns only those that
// change state. Useful for reducing the number of actions needed to check in
// tests.
func FilterActions(actions []kubetesting.Action) []kubetesting.Action {
	ignore := func(action kubetesting.Action) bool {
		for _, v := range []string{"list", "watch"} {
			for _, r := range []string{
				"shipmentorders",
				"releases",
				"clusters",
				"secrets",
				"installationtargets",
				"traffictargets",
				"capacitytargets",
			} {
				if action.Matches(v, r) {
					return true
				}
			}
		}

		return false
	}

	var ret []kubetesting.Action
	for _, action := range actions {
		if ignore(action) {
			continue
		}

		ret = append(ret, action)
	}

	return ret
}
