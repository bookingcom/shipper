package cmd

import (
	"bytes"
	"testing"

	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func TestPrintCountedRelease(t *testing.T) {
	tests := []struct {
		Name                   string
		Stdout                 *bytes.Buffer
		OutputReleases         []outputRelease
		OutputFlag             string
		ExpectedOutputReleases string
	}{
		{
			Name:                   "No releases to print",
			Stdout:                 bytes.NewBufferString(""),
			OutputReleases:         []outputRelease{},
			OutputFlag:             "json",
			ExpectedOutputReleases: "[]",
		},
		{
			Name:   "No output flag, no releases printed",
			Stdout: bytes.NewBufferString(""),
			OutputReleases: []outputRelease{
				{
					Namespace: "default",
					Name:      "super-server-8de83afb-0",
				},
			},
			ExpectedOutputReleases: "",
		},
		{
			Name:   "One release to print yaml",
			Stdout: bytes.NewBufferString(""),
			OutputReleases: []outputRelease{
				{
					Namespace: "default",
					Name:      "super-server-8de83afb-0",
				},
			},
			OutputFlag:             "yaml",
			ExpectedOutputReleases: "- name: super-server-8de83afb-0\n  namespace: default\n",
		},
		{
			Name:   "One release to print json",
			Stdout: bytes.NewBufferString(""),
			OutputReleases: []outputRelease{
				{
					Namespace: "default",
					Name:      "super-server-8de83afb-0",
				},
			},
			OutputFlag:             "json",
			ExpectedOutputReleases: "[\n    {\n        \"namespace\": \"default\",\n        \"name\": \"super-server-8de83afb-0\"\n    }\n]",
		},
	}

	for _, test := range tests {
		printOption = test.OutputFlag
		printCountedRelease(test.Stdout, test.OutputReleases)

		actualOutput := test.Stdout.String()
		if actualOutput != test.ExpectedOutputReleases {
			diff, err := shippertesting.YamlDiff(actualOutput, test.ExpectedOutputReleases)
			if err != nil {
				t.Fatalf(err.Error())
			}
			t.Fatalf(
				"release output differ from expected:\n%s",
				diff)
		}
	}
}
