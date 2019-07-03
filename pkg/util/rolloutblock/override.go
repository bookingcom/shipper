package rolloutblock

import (
	"sort"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	StatementSeparator = ","
)

type Override map[string]struct{}

func NewOverride(statement string) Override {
	override := make(Override)
	statements := strings.Split(statement, StatementSeparator)
	for _, s := range statements {
		if len(s) == 0 {
			continue
		}
		override[s] = struct{}{}
	}

	return override
}

func NewOverrideFromRolloutBlocks(rbs []*shipper.RolloutBlock) Override {
	o := make(Override)
	for _, item := range rbs {
		rbFullName := item.Namespace + "/" + item.Name
		o.Add(rbFullName)
	}
	return o
}

func (o Override) String() string {
	statements := make([]string, 0, len(o))
	for s := range o {
		statements = append(statements, s)
	}
	sort.Strings(statements)

	return strings.Join(statements, StatementSeparator)
}

func (o Override) Keys() []string {
	keys := make([]string, 0, len(o))
	for s := range o {
		keys = append(keys, s)
	}
	return keys
}

func (o Override) Delete(rm string) {
	delete(o, rm)
}

func (o Override) Add(statement string) {
	o[statement] = struct{}{}
}

func (o Override) Diff(o2 Override) Override {
	res := make(Override)
	for s := range o {
		if _, ok := o2[s]; !ok {
			res[s] = struct{}{}
		}
	}

	return res
}