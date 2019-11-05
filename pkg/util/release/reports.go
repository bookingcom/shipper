package release

import (
	"fmt"
	"sort"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func BuildClusterBreakdownReport(reports []shipper.ClusterCapacityReport) string {
	res := make([]string, 0, len(reports))
	for _, r := range reports {
		breakdown := r.Breakdown
		if len(breakdown) == 0 {
			continue
		}
		for _, sb := range breakdown {
			if len(sb.Containers) == 0 {
				continue
			}
			status := sb.Status
			typ := sb.Type
			contNames := make([]string, 0, len(sb.Containers))
			var totalcnt uint32
			for _, cont := range sb.Containers {
				var cnt uint32
				for _, s := range cont.States {
					cnt += s.Count
				}
				contNames = append(contNames, fmt.Sprintf("%s(x%d)", cont.Name, cnt))
				totalcnt += cnt
			}
			res = append(res, fmt.Sprintf(
				"%d container(s) [%s] are in status: %s:%s",
				totalcnt,
				strings.Join(contNames, ","),
				typ,
				status))
		}
	}

	sort.Strings(res)

	return strings.Join(res, "\n")
}

func BuildClusterProblemReport(s []shipper.PodStatus) string {
	res := make([]string, 0, len(s))
	for _, ps := range s {
		res = append(res, fmt.Sprintf(
			"%s:%s, reason: %s, message: %s",
			ps.Condition.Type,
			ps.Condition.Status,
			ps.Condition.Reason,
			ps.Condition.Message,
		))
	}

	sort.Strings(res)

	return strings.Join(res, "\n")
}
