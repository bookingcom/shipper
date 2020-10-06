package controller

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestSortReleasesByGeneration(t *testing.T) {
	expectedOrder := []string{"charlie", "beta", "delta", "alpha"}

	releases := []*shipper.Release{
		newRelease("alpha", "9"),
		newRelease("beta", "2"),
		newRelease("charlie", "1"),
		newRelease("delta", "6"),
	}

	sorted, err := SortReleasesByGeneration(releases)
	if err != nil {
		t.Fatalf("Failed to sort releases: %q", err)
	}

	sortedNames := make([]string, 0, len(sorted))
	for _, rel := range sorted {
		sortedNames = append(sortedNames, rel.GetName())
	}

	if fmt.Sprintf("%v", expectedOrder) != fmt.Sprintf("%v", sortedNames) {
		t.Fatalf("wrong sort order: expected %v but got %v", expectedOrder, sortedNames)
	}
}

func TestSortBrokenReleaseAnnotation(t *testing.T) {
	brokenReleases := []*shipper.Release{
		newRelease("alpha", "9"),
		newRelease("beta", "2"),
		newRelease("charlie", "1"),
		newRelease("delta", "asdfasdfsf"),
	}

	_, err := SortReleasesByGeneration(brokenReleases)
	if err == nil {
		t.Fatalf("failed to error on invalid release generation annotation")
	}
}

func newRelease(releaseName string, annotation string) *shipper.Release {
	return &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name: releaseName,
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: annotation,
			},
		},
	}
}
