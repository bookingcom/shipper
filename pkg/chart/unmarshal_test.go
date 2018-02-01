package chart

import (
	"testing"
)

const deploymentText = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-complex-app
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: my-complex-app
          image: "nginx:stable"
`

const somethingElseText = `
apiVersion: v1
kind: Service
metadata:
  name: my-complex-service
  namespace: default
spec:
  ports:
  - nodePort: 30682
    port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    app: my-complex-app
  type: ClusterIP
`

const garbage = `
apiVersion: huh?
kind: What
`

func TestGetDeploymentsValid(t *testing.T) {
	deployments := GetDeployments([]string{deploymentText, somethingElseText, garbage})
	if len(deployments) != 1 {
		t.Fatalf("expected exactly one Deployment but got %d", len(deployments))
	}

	d := deployments[0]

	const (
		expectedName      = "my-complex-app"
		expectedNamespace = "default"
		expectedReplicas  = 1
	)

	if d.GetName() != expectedName {
		t.Errorf("expected name %q but got %q", expectedName, d.GetName())
	}
	if d.GetNamespace() != expectedNamespace {
		t.Errorf("expected namespace %q but got %q", expectedNamespace, d.GetNamespace())
	}
	if *d.Spec.Replicas != expectedReplicas {
		t.Errorf("expected %d replicas but got %d", expectedReplicas, *d.Spec.Replicas)
	}
}
