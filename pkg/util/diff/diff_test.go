package diff

import (
	"fmt"
	"testing"
)

type Emptiness = bool

const (
	Empty    Emptiness = true
	NonEmpty           = false
)

type testDiff struct {
	empty Emptiness
}

var _ Diff = (*testDiff)(nil)

func newTestDiff(empty Emptiness) *testDiff {
	return &testDiff{
		empty: empty,
	}
}

func (d *testDiff) IsEmpty() bool {
	return d.empty
}

func (d *testDiff) String() string {
	return fmt.Sprintf("test diff: %t", d.empty)
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		Name     string
		Diff     MultiDiff
		Expected Emptiness
	}{
		{
			Name:     "empty list",
			Diff:     MultiDiff{},
			Expected: Empty,
		},
		{
			Name:     "single empty element",
			Diff:     MultiDiff{newTestDiff(Empty)},
			Expected: Empty,
		},
		{
			Name:     "single non-empty element",
			Diff:     MultiDiff{newTestDiff(NonEmpty)},
			Expected: NonEmpty,
		},
		{
			Name:     "nil element",
			Diff:     MultiDiff{nil},
			Expected: Empty,
		},
		{
			Name:     "empty and non-empty elements",
			Diff:     MultiDiff{newTestDiff(Empty), newTestDiff(NonEmpty)},
			Expected: NonEmpty,
		},
		{
			Name:     "non-empty element and nil",
			Diff:     MultiDiff{newTestDiff(NonEmpty), nil},
			Expected: NonEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			isEmpty := tt.Diff.IsEmpty()
			if isEmpty != tt.Expected {
				t.Errorf("Unexpected result returned by IsEmpty(): (diff: %#v): got: %t, want: %t", tt.Diff, isEmpty, tt.Expected)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		Name     string
		Diff     MultiDiff
		Expected string
	}{
		{
			Name:     "empty list",
			Diff:     MultiDiff{},
			Expected: "",
		},
		{
			Name:     "single empty element",
			Diff:     MultiDiff{newTestDiff(Empty)},
			Expected: "",
		},
		{
			Name:     "single non-empty element",
			Diff:     MultiDiff{newTestDiff(NonEmpty)},
			Expected: "test diff: false",
		},
		{
			Name:     "nil element",
			Diff:     MultiDiff{nil},
			Expected: "",
		},
		{
			Name:     "empty and non-empty elements",
			Diff:     MultiDiff{newTestDiff(Empty), newTestDiff(NonEmpty)},
			Expected: "test diff: false",
		},
		{
			Name:     "non-empty element and nil",
			Diff:     MultiDiff{newTestDiff(NonEmpty), nil},
			Expected: "test diff: false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			str := tt.Diff.String()
			if str != tt.Expected {
				t.Errorf("Unexpected result returned by String(): (diff: %#v): got: %s, want: %s", tt.Diff, str, tt.Expected)
			}
		})
	}
}
