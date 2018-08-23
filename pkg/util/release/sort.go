package release

import (
	"sort"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

type ByGenerationAscending []*v1.Release

func (a ByGenerationAscending) Len() int      { return len(a) }
func (a ByGenerationAscending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByGenerationAscending) Less(i, j int) bool {
	gi, _ := GetGeneration(a[i])
	gj, _ := GetGeneration(a[j])
	return gi < gj
}

type ByGenerationDescending []*v1.Release

func (a ByGenerationDescending) Len() int      { return len(a) }
func (a ByGenerationDescending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByGenerationDescending) Less(i, j int) bool {
	gi, _ := GetGeneration(a[i])
	gj, _ := GetGeneration(a[j])
	return gi > gj
}

func SortByGenerationAscending(rels []*v1.Release) []*v1.Release {
	relsCopy := make([]*v1.Release, len(rels))
	copy(relsCopy, rels)
	sort.Sort(ByGenerationAscending(relsCopy))
	return relsCopy
}

func SortByGenerationDescending(rels []*v1.Release) []*v1.Release {
	relsCopy := make([]*v1.Release, len(rels))
	copy(relsCopy, rels)
	sort.Sort(ByGenerationDescending(relsCopy))
	return relsCopy
}
