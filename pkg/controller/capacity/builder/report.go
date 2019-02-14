package builder

import (
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"sort"
)

type Report struct {
	ownerName  string
	breakdowns []v1alpha1.ClusterCapacityReportBreakdown
}

func NewReport(ownerName string) *Report {
	return &Report{ownerName: ownerName}
}

func (r *Report) AddBreakdown(breakdown v1alpha1.ClusterCapacityReportBreakdown) *Report {
	r.breakdowns = append(r.breakdowns, breakdown)
	return r
}

func (r *Report) Build() *v1alpha1.ClusterCapacityReport {

	sort.Slice(r.breakdowns, func(i, j int) bool {
		return r.breakdowns[i].Type < r.breakdowns[j].Type
	})

	report := v1alpha1.ClusterCapacityReport{
		Owner: v1alpha1.ClusterCapacityReportOwner{
			Name: r.ownerName,
		},
		Breakdown: r.breakdowns,
	}
	return &report
}
