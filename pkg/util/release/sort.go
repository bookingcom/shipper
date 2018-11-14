package release

import (
	"sort"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ByGenerationAscending []*shipper.Release

func (a ByGenerationAscending) Len() int      { return len(a) }
func (a ByGenerationAscending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByGenerationAscending) Less(i, j int) bool {
	gi, _ := GetGeneration(a[i])
	gj, _ := GetGeneration(a[j])
	return gi < gj
}

type ByGenerationDescending []*shipper.Release

func (a ByGenerationDescending) Len() int      { return len(a) }
func (a ByGenerationDescending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByGenerationDescending) Less(i, j int) bool {
	gi, _ := GetGeneration(a[i])
	gj, _ := GetGeneration(a[j])
	return gi > gj
}

func SortByGenerationAscending(rels []*shipper.Release) []*shipper.Release {
	relsCopy := make([]*shipper.Release, len(rels))
	copy(relsCopy, rels)
	sort.Sort(ByGenerationAscending(relsCopy))
	return relsCopy
}

func SortByGenerationDescending(rels []*shipper.Release) []*shipper.Release {
	relsCopy := make([]*shipper.Release, len(rels))
	copy(relsCopy, rels)
	sort.Sort(ByGenerationDescending(relsCopy))
	return relsCopy
}
