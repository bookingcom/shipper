package testing

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	"github.com/pmezard/go-difflib/difflib"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetesting "k8s.io/client-go/testing"
)

const (
	NoResyncPeriod time.Duration = 0

	ContextLines = 4

	TestNamespace = "test-namespace"
	TestLabel     = "shipper-e2e-test"

	TestRegion = "eu-west"
)

// CheckActions takes a slice of expected actions and a slice of observed
// actions (typically obtained from fakeClient.Actions()) and compares them.
// Calls Errorf on t for every difference it finds.
func CheckActions(expected, actual []kubetesting.Action, t *testing.T) {
	for i, action := range actual {
		if len(expected) < i+1 {
			t.Errorf("%d unexpected actions:", len(actual)-len(expected))
			for _, unexpectedAction := range actual[i:] {
				t.Logf("\n%s", prettyPrintAction(unexpectedAction))
			}
			break
		}

		CheckAction(expected[i], action, t)
	}

	if len(expected) > len(actual) {
		t.Errorf("missing %d expected actions:", len(expected)-len(actual))
		for _, missingExpectedAction := range expected[len(actual):] {
			t.Logf("\n%s", prettyPrintAction(missingExpectedAction))
		}
	}
}

// ShallowCheckActions takes a slice of expected actions and a slice of observed
// actions (typically obtained from fakeClient.Actions()) and compares them
// shallowly. Calls Errorf on t for every difference it finds.
func ShallowCheckActions(expected, actual []kubetesting.Action, t *testing.T) {
	for i, action := range actual {
		if len(expected) < i+1 {
			t.Errorf("%d unexpected actions: %+v", len(actual)-len(expected), actual[i:])
			for _, unexpectedAction := range actual[i:] {
				t.Logf("\n%s", prettyPrintAction(unexpectedAction))
			}
			break
		}

		ShallowCheckAction(expected[i], action, t)
	}

	if len(expected) > len(actual) {
		t.Errorf("missing %d expected actions: %+v", len(expected)-len(actual), expected[len(actual):])
	}
}

// ShallowCheckAction checks the verb, resource, and namespace without looking
// at the objects involved. This is a stand-in until we port the Installation
// controller to not use 'nil' as the object involved in the kubetesting.Actions
// it expects.
func ShallowCheckAction(expected, actual kubetesting.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) &&
		actual.GetSubresource() == expected.GetSubresource() &&
		actual.GetResource() == expected.GetResource()) {

		t.Errorf("expected\n\t%#v\ngot\n\t%#v", expected, actual)
		return
	}

	if expected.GetNamespace() != actual.GetNamespace() {
		t.Errorf("expected action in ns %q, got ns %q", expected.GetNamespace(), actual.GetNamespace())
		return
	}

	if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
		t.Errorf("expected action %T but got %T", expected, actual)
		return
	}
}

// CheckAction compares two individual actions and calls Errorf on t if it finds
// a difference.
func CheckAction(expected, actual kubetesting.Action, t *testing.T) {
	prettyExpected := prettyPrintAction(expected)
	prettyActual := prettyPrintAction(actual)

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(prettyExpected),
		B:        difflib.SplitLines(prettyActual),
		FromFile: "Expected Action",
		ToFile:   "Actual Action",
		// TODO(btyler): add a param or env var to change context size.
		Context: ContextLines,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		panic("could not compute diff! this is bad news bears")
	}

	if len(text) > 0 {
		t.Errorf("expected action is different from actual:\n%s", text)
	}
}

// PrettyPrintActions pretty-prints a slice of actions, useful for
// creating a human-readable list for debugging.
func PrettyPrintActions(actions []kubetesting.Action, t *testing.T) {
	for _, action := range actions {
		t.Logf("\n%s", prettyPrintAction(action))
	}
}

// FilterActions, given a slice of observed actions, returns only those that
// change state. Useful for reducing the number of actions needed to check in
// tests.
func FilterActions(actions []kubetesting.Action) []kubetesting.Action {
	ignore := func(action kubetesting.Action) bool {
		for _, v := range []string{"list", "watch"} {
			for _, r := range []string{
				"applications",
				"shipmentorders",
				"releases",
				"clusters",
				"secrets",
				"installationtargets",
				"traffictargets",
				"capacitytargets",
				"deployments",
				"services",
				"pods",
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

func CheckEvents(expectedOrderedEvents []string, receivedEvents []string, t *testing.T) {
	if !reflect.DeepEqual(expectedOrderedEvents, receivedEvents) {
		t.Errorf("Events don't match expectation:\n\n%s", cmp.Diff(expectedOrderedEvents, receivedEvents))
	}
}

func prettyPrintAction(a kubetesting.Action) string {
	verb := a.GetVerb()
	gvk := a.GetResource()
	ns := a.GetNamespace()

	template := fmt.Sprintf("Verb: %s\nGVK: %s\nNamespace: %s\n--------\n%%s", verb, gvk.String(), ns)

	switch action := a.(type) {

	case kubetesting.CreateActionImpl:
		obj, err := yaml.Marshal(action.GetObject())
		if err != nil {
			panic(fmt.Sprintf("could not marshal %+v: %q", action.GetObject(), err))
		}

		return fmt.Sprintf(template, string(obj))

	case kubetesting.UpdateActionImpl:
		obj, err := yaml.Marshal(action.GetObject())
		if err != nil {
			panic(fmt.Sprintf("could not marshal %+v: %q", action.GetObject(), err))
		}

		return fmt.Sprintf(template, string(obj))

	case kubetesting.PatchActionImpl:
		return fmt.Sprintf(template, string(action.GetPatch()))

	case kubetesting.GetActionImpl:
		message := fmt.Sprintf("(no object body: GET %s)", action.GetName())
		return fmt.Sprintf(template, message)

	case kubetesting.DeleteActionImpl:
		message := fmt.Sprintf("(no object body: DELETE %s)", action.GetName())
		return fmt.Sprintf(template, message)

	case kubetesting.ActionImpl:
		message := fmt.Sprintf("(no object body: %s %s)", action.GetVerb(), action.GetResource())
		return fmt.Sprintf(template, message)
	}

	panic(fmt.Sprintf("unknown action! patch printAction to support %T %+v", a, a))
}

func NewDiscoveryAction(_ string) kubetesting.ActionImpl {
	// FakeDiscovery has a very odd way of generating fake actions for
	// discovery. We try to paper over that as best we can. The ignored
	// parameter is trying to be future proof in case FakeDiscovery ever
	// decides to fix its nasty ways and actually report the resource we're
	// trying to discover.
	return kubetesting.ActionImpl{
		Verb:     "get",
		Resource: schema.GroupVersionResource{Resource: "resource"},
	}
}
