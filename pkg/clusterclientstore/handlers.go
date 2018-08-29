package clusterclientstore

import (
	kubeinformers "k8s.io/client-go/informers"
)

// SubscriptionRegisterFunc should call the relevant functions on a shared
// informer factory to set up watches.
//
// Note that there should be no event handlers being assigned to any informers
// in this function.
type SubscriptionRegisterFunc func(kubeinformers.SharedInformerFactory)

// EventHandlerRegisterFunc is called after the caches for the clusters have
// been built, and provides a hook for a controller to register its event
// handlers. These will be event handlers for changes to the resources that the
// controller has subscribed to in the `SubscriptionRegisterFunc` callback.
type EventHandlerRegisterFunc func(kubeinformers.SharedInformerFactory, string)
