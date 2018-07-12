package chart

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/helm/pkg/chartutil"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func TestRender(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	chartFile, err := os.Open(filepath.Join(cwd, "testdata", "my-complex-app-0.2.0.tgz"))
	if err != nil {
		t.Fatal(err)
	}

	chart, err := chartutil.LoadArchive(chartFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedReplicas := 42
	vals := &shipperv1.ChartValues{
		"replicaCount": expectedReplicas,
	}

	rendered, err := Render(chart, "my-complex-app", "my-complex-app", vals)
	if err != nil {
		t.Fatal(err)
	}

	deployments := GetDeployments(rendered)
	extractedReplicas := deployments[0].Spec.Replicas
	if extractedReplicas == nil {
		t.Fatal("extracted nil replicas from deployment")
	}
	actualReplicas := int(*extractedReplicas)
	if actualReplicas != expectedReplicas {
		t.Errorf("expected %d replicas but found %d", expectedReplicas, actualReplicas)
	}
}

func TestRenderZeroByteTemplates(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	chartFile, err := os.Open(filepath.Join(cwd, "testdata", "my-complex-app-0.2.0.tgz"))
	if err != nil {
		t.Fatal(err)
	}

	testCases := []string{
		"",
		"\n",
		"   \n   \n\t",
		"apiVersion: v1\nkind: Service",
	}

	expected := []string{
		"",
		"",
		"",
		"apiVersion: v1\nkind: Service",
	}

	for i, testCase := range testCases {
		// need to reset the file position to 0 to reload
		chartFile.Seek(0, 0)
		chart, err := chartutil.LoadArchive(chartFile)
		if err != nil {
			t.Fatal(err)
		}

		for _, template := range chart.Templates {
			if template.Name == "templates/service.yaml" {
				template.Data = []byte(testCase)
			}
		}

		rendered, err := Render(chart, "my-complex-app", "my-complex-app", &shipperv1.ChartValues{})
		if err != nil {
			t.Fatalf("failed to render test case %q: %s", testCase, err)
		}

		if expected[i] == "" {
			if len(rendered) != 1 {
				t.Fatalf("expected chart.Render to strip out an object with contents %q, but it did not. rendered stuff: %v", testCase, rendered)
			}
		} else {
			if len(rendered) == 1 {
				t.Fatalf("chart.Render stripped out an object it should not have: contents %q", testCase)
			}
		}
	}
}
