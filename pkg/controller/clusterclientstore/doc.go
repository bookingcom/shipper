// package clusterclientstore provides a thread-safe storage for kubernetes
// clients. The internal storage is updated automatically by observing
// the kubernetes cluster for cluster objects. New cluster objects
// trigger a client creation, updates to Secret objects trigger
// re-creation of a client, and Cluster deletions cause the removal of
// a client.
//
// Important Note
//
// before using the methods for retrieving cluster-specific objects from the store, you *must* call the `Run()` method.
package clusterclientstore
