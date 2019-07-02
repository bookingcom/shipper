package string

func Grep(strings []string, s string) []string {
	for i, item := range strings {
		if item == s {
			return append(strings[:i], strings[i+1:]...)
		}
	}
	return strings
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