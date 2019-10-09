package diff

import "strings"

type Diff interface {
	IsEmpty() bool
	String() string
}

type MultiDiff []Diff

var _ Diff = (MultiDiff)(nil)

func (md MultiDiff) IsEmpty() bool {
	if len(md) == 0 {
		return true
	}
	for _, d := range md {
		if !d.IsEmpty() {
			return false
		}
	}
	return true
}

func (md MultiDiff) String() string {
	if md.IsEmpty() {
		return ""
	}
	b := make([]string, 0, len(md))
	for _, d := range md {
		if s := d.String(); s != "" {
			b = append(b, s)
		}
	}
	return strings.Join(b, ", ")
}

func (md *MultiDiff) Append(d Diff) {
	*md = append(*md, d)
}
