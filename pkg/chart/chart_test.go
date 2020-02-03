package chart

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"helm.sh/helm/v3/pkg/chart/loader"
)

func TestRender(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	chartFile, err := os.Open(filepath.Join(cwd, "testdata", "my-complex-app-0.2.0.tgz"))
	if err != nil {
		t.Fatal(err)
	}

	chart, err := loader.LoadArchive(chartFile)
	if err != nil {
		t.Fatal(err)
	}

	expectedReplicas := 42
	vals := &shipper.ChartValues{
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

func TestCollectObjects(t *testing.T) {
	testCases := []map[string]string{
		map[string]string{"foo.yaml": ""},
		map[string]string{"foo.yaml": "\n"},
		map[string]string{"foo.yaml": "   \n   \n\t"},
		map[string]string{"foo.yaml": "apiVersion: v1\nkind: Service"},
		map[string]string{"foo.yaml": "apiVersion: v1\nkind: Service\n---\napiVersion: v1\nkind: Pod"},
	}

	expected := [][]string{
		[]string{},
		[]string{},
		[]string{},
		[]string{"apiVersion: v1\nkind: Service"},
		[]string{"apiVersion: v1\nkind: Service", "apiVersion: v1\nkind: Pod"},
	}

	for i, testCase := range testCases {
		got := CollectObjects(testCase)
		if !reflect.DeepEqual(expected[i], got) {
			t.Fatalf("expected %q to produce %q, but got %q", testCase, expected[i], got)
		}
	}
}
