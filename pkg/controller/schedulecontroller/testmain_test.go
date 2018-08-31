package schedulecontroller

import (
	"log"
	"os"
	"testing"

	"k8s.io/helm/pkg/repo/repotest"
)

var chartRepoURL string

func TestMain(m *testing.M) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		log.Fatal(err)
	}

	chartRepoURL = srv.URL()

	result := m.Run()

	// If the test panics for any reason, the test server doesn't get cleaned up.
	os.RemoveAll(hh.String())
	srv.Stop()

	os.Exit(result)
}
