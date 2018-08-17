package release

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

type ByGenerationAscending []*v1.Release

func (a ByGenerationAscending) Len() int      { return len(a) }
func (a ByGenerationAscending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByGenerationAscending) Less(i, j int) bool {
	gi, _ := GetGeneration(a[i])
	gj, _ := GetGeneration(a[j])
	return gi > gj
}

type ByGenerationDescending []*v1.Release

func (a ByGenerationDescending) Len() int      { return len(a) }
func (a ByGenerationDescending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByGenerationDescending) Less(i, j int) bool {
	gi, _ := GetGeneration(a[i])
	gj, _ := GetGeneration(a[j])
	return gi < gj
}
