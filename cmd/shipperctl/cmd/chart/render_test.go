package chart

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func TestRender(t *testing.T) {
	tests := []struct {
		Name         string
		C            ChartRenderConfig
		ExpectedYaml string
		ExpectedErr  error
	}{
		{
			Name: "Working chart",
			C: ChartRenderConfig{
				ChartSpec: shipper.Chart{
					Name:    "nginx",
					Version: "0.0.1",
					RepoURL: "https://raw.githubusercontent.com/bookingcom/shipper/master/test/e2e/testdata",
				},
				ChartValues: &shipper.ChartValues{
					"replicaCount": 1,
				},
				Namespace:   "",
				ReleaseName: "super-server-foobar-0",
			},
			ExpectedYaml: "apiVersion: v1\nkind: Service\nmetadata:\n  name: nginx\n  labels:\n    app: super-server-foobar-0-nginx\n    chart: \"nginx-0.0.1\"\n    release: \"super-server-foobar-0\"\n    heritage: \"Tiller\"\nspec:\n  type: ClusterIP\n  ports:\n    - name: http\n      port: 80\n      targetPort: http\n  selector:\n    shipper-app: super-server-foobar-0-nginx%s\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: super-server-foobar-0-nginx\n  labels:\n    app: super-server-foobar-0-nginx\n    chart: \"nginx-0.0.1\"\n    release: \"super-server-foobar-0\"\n    heritage: \"Tiller\"\nspec:\n  selector:\n    matchLabels:\n      shipper-app: super-server-foobar-0-nginx\n      shipper-release: \"super-server-foobar-0\"\n  replicas: 1\n  template:\n    metadata:\n      labels:\n        app: super-server-foobar-0-nginx\n        chart: \"nginx-0.0.1\"\n        release: \"super-server-foobar-0\"\n        heritage: \"Tiller\"\n    spec:\n      containers:\n      - name: super-server-foobar-0-nginx\n        image: \"docker.io/bitnami/nginx:1.14.2\"\n        imagePullPolicy: \"IfNotPresent\"\n        ports:\n        - name: http\n          containerPort: 8080\n        livenessProbe:\n          httpGet:\n            path: /\n            port: http\n          initialDelaySeconds: 30\n          timeoutSeconds: 5\n          failureThreshold: 6\n        readinessProbe:\n          httpGet:\n            path: /\n            port: http\n          initialDelaySeconds: 5\n          timeoutSeconds: 3\n          periodSeconds: 5\n        volumeMounts:\n      volumes:",
			ExpectedErr:  nil,
		},
		{
			Name: "Invalid chart template",
			C: ChartRenderConfig{
				ChartSpec: shipper.Chart{
					Name:    "invalid-chart-template",
					Version: "0.0.1",
					RepoURL: "https://raw.githubusercontent.com/bookingcom/shipper/master/test/e2e/testdata",
				},
				ChartValues: &shipper.ChartValues{},
				Namespace:   "",
				ReleaseName: "",
			},
			ExpectedYaml: "",
			ExpectedErr:  fmt.Errorf("could not render the chart: render error in \"invalid-chart-template/templates/deployment.yaml\": template: invalid-chart-template/templates/deployment.yaml:4:20: executing \"invalid-chart-template/templates/deployment.yaml\" at <{{template \"some-nonsense.fullname\" .}}>: template \"some-nonsense.fullname\" not defined"),
		},
		{
			Name: "Invalid chart url",
			C: ChartRenderConfig{
				ChartSpec: shipper.Chart{
					Name:    "0",
					Version: "0.0.1",
					RepoURL: "0",
				},
				ChartValues: &shipper.ChartValues{},
				Namespace:   "",
				ReleaseName: "",
			},
			ExpectedYaml: "",
			ExpectedErr:  shippererrors.NewChartRepoInternalError(&url.Error{
				Op:  "parse",
				URL: "0",
				Err: fmt.Errorf("invalid URI for request"),
			}),
		},
	}

	for _, test := range tests {
		actualYaml, actualErr := render(test.C)
		if !reflect.DeepEqual(actualErr, test.ExpectedErr) {
			t.Fatalf("expected error \n%v\ngot error \n%v", test.ExpectedErr, actualErr)
		}
		eq, diff := shippertesting.DeepEqualDiff(actualYaml, test.ExpectedYaml)
		if !eq {
			t.Fatalf("rendered chart differ from expected:\n%s", diff)
		}
	}
}
