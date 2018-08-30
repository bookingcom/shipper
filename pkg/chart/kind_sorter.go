package chart

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

// SortOrder is an ordering of Kinds.
type SortOrder []string

// InstallOrder is the order in which manifests should be installed (by Kind).
//
// Those occurring earlier in the list get installed before those occurring later in the list.
var InstallOrder SortOrder = []string{
	"Namespace",
	"ResourceQuota",
	"LimitRange",
	"Secret",
	"ConfigMap",
	"StorageClass",
	"PersistentVolume",
	"PersistentVolumeClaim",
	"ServiceAccount",
	"CustomResourceDefinition",
	"ClusterRole",
	"ClusterRoleBinding",
	"Role",
	"RoleBinding",
	"Service",
	"DaemonSet",
	"Pod",
	"ReplicationController",
	"ReplicaSet",
	"Deployment",
	"StatefulSet",
	"Job",
	"CronJob",
	"Ingress",
	"APIService",
}

// UninstallOrder is the order in which manifests should be uninstalled (by Kind).
//
// Those occurring earlier in the list get uninstalled before those occurring later in the list.
var UninstallOrder SortOrder = []string{
	"APIService",
	"Ingress",
	"Service",
	"CronJob",
	"Job",
	"StatefulSet",
	"Deployment",
	"ReplicaSet",
	"ReplicationController",
	"Pod",
	"DaemonSet",
	"RoleBinding",
	"Role",
	"ClusterRoleBinding",
	"ClusterRole",
	"CustomResourceDefinition",
	"ServiceAccount",
	"PersistentVolumeClaim",
	"PersistentVolume",
	"StorageClass",
	"ConfigMap",
	"Secret",
	"LimitRange",
	"ResourceQuota",
	"Namespace",
}

type extendedManifest struct {
	manifest        string
	gvk             *schema.GroupVersionKind
	decodedManifest runtime.Object
	object          metav1.Object
}

type kindSorter struct {
	ordering          map[string]int
	extendedManifests []extendedManifest
}

func newKindSorter(m []string, s SortOrder) (*kindSorter, error) {
	o := make(map[string]int, len(s))
	for v, k := range s {
		o[k] = v
	}

	var ems []extendedManifest
	for _, s := range m {
		if decodedManifest, gvk, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(s), nil, nil); err != nil {
			return nil, fmt.Errorf("could not decode manifest: %s", err)
		} else if object, ok := decodedManifest.(metav1.Object); !ok {
			return nil, fmt.Errorf("object does not implement metaV1.Object")
		} else {
			e := extendedManifest{decodedManifest: decodedManifest, gvk: gvk, object: object, manifest: s}
			ems = append(ems, e)
		}
	}

	return &kindSorter{
		ordering:          o,
		extendedManifests: ems,
	}, nil
}

func (k *kindSorter) Manifests() []string {
	var manifests []string
	for _, em := range k.extendedManifests {
		manifests = append(manifests, em.manifest)
	}
	return manifests
}

func (k *kindSorter) Len() int { return len(k.extendedManifests) }

func (k *kindSorter) Swap(i, j int) {
	k.extendedManifests[i], k.extendedManifests[j] = k.extendedManifests[j], k.extendedManifests[i]
}

func (k *kindSorter) Less(i, j int) bool {
	a := k.extendedManifests[i]
	b := k.extendedManifests[j]

	first, aok := k.ordering[a.gvk.Kind]
	second, bok := k.ordering[b.gvk.Kind]

	if first == second {
		if !aok && !bok && a.gvk.Kind != b.gvk.Kind {
			return a.gvk.Kind < b.gvk.Kind
		}
		return a.object.GetName() < b.object.GetName()
	}

	if !aok {
		return false
	}

	if !bok {
		return true
	}

	return first < second
}
