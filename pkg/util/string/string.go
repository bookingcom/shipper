package string

func AppendIfMissing(strings []string, s string) []string {
	m := make(map[string]bool)
	for _, item := range strings {
		m[item] = true
	}
	if _, ok := m[s]; !ok {
		return append(strings, s)
	}
	return strings
}

func Grep(strings []string, s string) []string {
	for i, item := range strings {
		if item == s {
			return append(strings[:i], strings[i+1:]...)
		}
	}
	return strings
}

// compare two string slices as set
func Equal(s1, s2 []string) bool {
	if len(s1) != len(s2) {
		return false
	}

	m := make(map[string]bool)
	for _, item := range s1 {
		m[item] = true
	}

	for _, item := range s2 {
		if _, ok := m[item]; !ok {
			return false
		}
	}

	m = make(map[string]bool)
	for _, item := range s2 {
		m[item] = true
	}

	for _, item := range s1 {
		if _, ok := m[item]; !ok {
			return false
		}
	}

	return true
}

// Set difference: a - b
func SetDifference(a []string, b []string) []string {
	if len(b) == 0 || len(a) == 0 {
		return a
	}

	var diff []string
	m := make(map[string]bool)

	for _, item := range b {
		m[item] = true
	}

	for _, item := range a {
		if _, ok := m[item]; !ok {
			diff = append(diff, item)
		}
	}

	return diff
}