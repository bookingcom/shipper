package testing

import (
	"fmt"
	"reflect"

	"github.com/pmezard/go-difflib/difflib"
	"sigs.k8s.io/yaml"
)

const ContextLines = 4

func YamlDiff(a interface{}, b interface{}) (string, error) {
	yamlActual, err := yaml.Marshal(a)
	if err != nil {
		return "", err
	}

	yamlExpected, err := yaml.Marshal(b)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(yamlExpected)),
		B:        difflib.SplitLines(string(yamlActual)),
		FromFile: "Expected",
		ToFile:   "Actual",
		Context:  ContextLines,
	}

	return difflib.GetUnifiedDiffString(diff)
}

func DeepEqualDiff(expected, actual interface{}) (bool, string) {
	if !reflect.DeepEqual(actual, expected) {
		diff, err := YamlDiff(actual, expected)
		if err != nil {
			panic(fmt.Sprintf("couldn't generate yaml diff: %s", err))
		}

		return false, diff
	}

	return true, ""
}
