package chart

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
)

func GetDeployments(rawRendered []string) []appsv1.Deployment {
	var deployments []appsv1.Deployment

	decoder := scheme.Codecs.UniversalDeserializer()

	for _, raw := range rawRendered {
		klog.V(10).Infof("attempting to decode %q", raw)

		var d appsv1.Deployment
		obj, _, err := decoder.Decode([]byte(raw), nil, &d)
		if err != nil {
			klog.Warningf("failed to unmarshal a deployment: %s", err)
			continue
		}

		const expectedKind = "Deployment"
		gotKind := obj.GetObjectKind().GroupVersionKind().Kind
		if gotKind != expectedKind {
			klog.V(10).Infof("got a %q, skipping", gotKind)
			continue
		}

		deployments = append(deployments, d)
	}

	return deployments
}
