package chart

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func TestDownload(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		srv.Stop()
		os.RemoveAll(hh.String())
	}()

	inChart := shipperv1.Chart{
		Name:    "my-complex-app",
		Version: "0.2.0",
		RepoURL: srv.URL(),
	}

	_, err = Download(inChart)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRender(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	chart, err := os.Open(filepath.Join(cwd, "testdata", "my-complex-app-0.2.0.tgz"))
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
