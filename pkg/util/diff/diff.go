package diff

import "strings"

type Diff interface {
	IsEmpty() bool
	String() string
}

type MultiDiff []Diff

var _ Diff = (MultiDiff)(nil)

func NewMultiDiff() *MultiDiff {
	return new(MultiDiff)
}

func (md MultiDiff) IsEmpty() bool {
	if len(md) == 0 {
		return true
	}
	for _, d := range md {
		// Explicit nil-check is mandatory as it's an interface type
		// and a nil-receiver only works for struct pointers.
		if d != nil && !d.IsEmpty() {
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
		// Explicit nil-check is mandatory as it's an interface type
		// and a nil-receiver only works for struct pointers.
		if d == nil || d.IsEmpty() {
			continue
		}
		if s := d.String(); s != "" {
			b = append(b, s)
		}
	}
	return strings.Join(b, ", ")
}

func (md *MultiDiff) Append(d Diff) {
	*md = append(*md, d)
}
