package chart

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func TestDownload(t *testing.T) {
	dir, err := ioutil.TempDir("", "shipper-api-downloadchart-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := ensureDirectories(helmpath.Home(dir)); err != nil {
		t.Fatal(err)
	}

	srv := repotest.NewServer(dir)
	defer srv.Stop()
	if _, err := srv.CopyCharts("testdata/*.tgz"); err != nil {
		t.Error(err)
		return
	}
	if err := srv.LinkIndices(); err != nil {
		t.Fatal(err)
	}

	inChart := shipperv1.Chart{
		Name:    "my-complex-app",
		Version: "0.1.0",
		RepoURL: srv.URL(),
	}

	_, err = Download(inChart)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRender(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	chart, err := os.Open(filepath.Join(cwd, "testdata", "my-complex-app-0.1.0.tgz"))
	if err != nil {
		t.Fatal(err)
	}

	vals := &shipperv1.ChartValues{
		"foo": "bar",
	}

	if _, err := Render(chart, "my-complex-app", "my-complex-app", vals); err != nil {
		t.Fatal(err)
	}
}

func ensureDirectories(hh helmpath.Home) error {
	for _, p := range []string{
		hh.String(),
		hh.Repository(),
		hh.Cache(),
	} {
		if err := os.MkdirAll(p, 0755); err != nil {
			return fmt.Errorf("could not create %s: %s", p, err)
		}
	}

	return nil
}
