package release

import (
	"fmt"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func buildRelease(namespace, name string, generation string) *shipper.Release {
	return &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: name,
				shipper.AppLabel:     "test-application",
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: generation,
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: "local"}},
				},
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionFalse},
			},
		},
	}
}

func TestGetSiblingReleasesOriginPresent(t *testing.T) {
	numReleases := 5
	releases := make([]*shipper.Release, 0, numReleases)
	ns := "test-namespace"

	buildReleaseWithGen := func(gen int) *shipper.Release {
		name := fmt.Sprintf("test-release-%d", gen)
		return buildRelease(ns, name, strconv.Itoa(gen))
	}

	for i := 0; i < numReleases; i++ {
		releases = append(releases, buildReleaseWithGen(i))
	}

	getRelByIx := func(ix int) *shipper.Release {
		if ix < 0 || ix >= len(releases) {
			return nil
		}
		return releases[ix]
	}
	tests := [][3]int{ // format: inspectee ix, predecessor ix, ancestor ix; -1 stands for none
		{0, -1, 1},
		{1, 0, 2},
		{2, 1, 3},
		{3, 2, 4},
		{4, 3, -1},
	}

	for _, tt := range tests {
		predecessor, ancestor, _ := GetSiblingReleases(releases[tt[0]], releases)
		if !equality.Semantic.DeepEqual(predecessor, getRelByIx(tt[1])) {
			t.Errorf("unexpected predecessor for release %s: want: %v, got: %v", releases[tt[0]].Name, getRelByIx(tt[1]), predecessor)
		}
		if !equality.Semantic.DeepEqual(ancestor, getRelByIx(tt[2])) {
			t.Errorf("unexpected ancestor for release %s: want: %v, got: %v", releases[tt[0]].Name, getRelByIx(tt[2]), ancestor)
		}
	}
}

func TestGetSiblingReleasesOriginAbsent(t *testing.T) {
	numReleases := 5
	releases := make([]*shipper.Release, 0, numReleases)
	ns := "test-namespace"

	buildReleaseWithGen := func(gen int) *shipper.Release {
		name := fmt.Sprintf("test-release-%d", gen)
		return buildRelease(ns, name, strconv.Itoa(gen))
	}

	for i := 0; i < numReleases; i++ {
		releases = append(releases, buildReleaseWithGen(i*10)) // extra gen num padding
	}

	getRelByIx := func(ix int) *shipper.Release {
		if ix < 0 || ix >= len(releases) {
			return nil
		}
		return releases[ix]
	}
	tests := [][3]int{ // format: inspectee ix, predecessor ix, ancestor ix; -1 stands for none
		{0, 0, 1},
		{1, 1, 2},
		{2, 2, 3},
		{3, 3, 4},
		{4, 4, -1}, // this case is tricky: if the example release is out of the range, the method returns no predecessor
	}

	for _, tt := range tests {
		rel := buildReleaseWithGen(tt[0]*10 + 5) // gen numbers are padded by 10 and +5 moves it in between
		predecessor, ancestor, _ := GetSiblingReleases(rel, releases)
		if !equality.Semantic.DeepEqual(predecessor, getRelByIx(tt[1])) {
			t.Errorf("unexpected predecessor for release %s: want: %v, got: %v", rel.Name, getRelByIx(tt[1]), predecessor)
		}
		if !equality.Semantic.DeepEqual(ancestor, getRelByIx(tt[2])) {
			t.Errorf("unexpected ancestor for release %s: want: %v, got: %v", rel.Name, getRelByIx(tt[2]), ancestor)
		}
	}
}
