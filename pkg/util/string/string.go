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

func Delete(strings []string, s string) []string {
	for i, item := range strings {
		if item == s {
			return append(strings[:i], strings[i+1:]...)
		}
	}
	return strings
}
