package rolloutblock

import (
	"sort"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	StatementSeparator = ","
)

type ObjectNameList map[string]struct{}

func NewObjectNameList(statement string) ObjectNameList {
	override := make(ObjectNameList)
	statements := strings.Split(statement, StatementSeparator)
	for _, s := range statements {
		if len(s) == 0 {
			continue
		}
		override[s] = struct{}{}
	}

	return override
}

func NewObjectNameListFromRolloutBlocksList(rbs []*shipper.RolloutBlock) ObjectNameList {
	o := make(ObjectNameList)
	for _, item := range rbs {
		rbFullName := item.Namespace + "/" + item.Name
		o.Add(rbFullName)
	}
	return o
}

func (o ObjectNameList) String() string {
	statements := make([]string, 0, len(o))
	for s := range o {
		statements = append(statements, s)
	}
	sort.Strings(statements)

	return strings.Join(statements, StatementSeparator)
}

func (o ObjectNameList) Keys() []string {
	keys := make([]string, 0, len(o))
	for s := range o {
		keys = append(keys, s)
	}
	return keys
}

func (o ObjectNameList) Delete(rm string) {
	delete(o, rm)
}

func (o ObjectNameList) Add(statement string) {
	o[statement] = struct{}{}
}

func (o ObjectNameList) AddMultiple(o2 ObjectNameList) {
	for s := range o2 {
		o.Add(s)
	}
}

func (o ObjectNameList) Diff(o2 ObjectNameList) ObjectNameList {
	res := make(ObjectNameList)
	for s := range o {
		if _, ok := o2[s]; !ok {
			res[s] = struct{}{}
		}
	}

	return res
}
