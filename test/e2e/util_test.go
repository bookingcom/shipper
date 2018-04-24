package e2e

import (
	"time"
	"unicode"
)

func kebabCaseName(testName string) string {
	runes := make([]rune, 0, len(testName))
	for _, r := range testName {
		if unicode.IsUpper(r) {
			runes = append(runes, '-')
		}
		runes = append(runes, unicode.ToLower(r))
	}
	// strip off that first '-'
	return string(runes[1:])
}

func stopAfter(t time.Duration) <-chan struct{} {
	stopCh := make(chan struct{})
	go func() {
		<-time.After(t)
		close(stopCh)
	}()
	return stopCh
}
