package controller

import (
	"math/rand"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetRandomDuration(resyncPeriod time.Duration) time.Duration {
	if resyncPeriod == 0 {
		// protect rand from panicking
		return 0
	}
	duration := time.Duration(rand.Intn(int(resyncPeriod)))
	return duration
}

func CalculateDuration(a, b interface{}, resyncPeriod, defaultValue time.Duration) time.Duration {
	aObj, ok := a.(v1.Object)
	if !ok {
		return defaultValue
	}
	bObj, ok := b.(v1.Object)
	if !ok {
		return defaultValue
	}

	if aObj.GetResourceVersion() == bObj.GetResourceVersion() {
		return GetRandomDuration(resyncPeriod)
	}

	return defaultValue
}
