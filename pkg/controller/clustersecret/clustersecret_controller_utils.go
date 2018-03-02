package clustersecret

import (
	"encoding/hex"
	"fmt"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func (c *Controller) processCluster(cluster *shipperv1.Cluster) error {
	// cluster is not a copy here!
	// this controller must not modify the Cluster resource

	secretName := secretNameForCluster(cluster)
	secret, err := c.secretLister.Secrets(c.ownNamespace).Get(secretName)
	if err != nil {
		if errors.IsNotFound(err) {
			// either a new Cluster or the Secret is gone, re-create it
			var crt, key, csum []byte
			crt, key, csum, err = c.tls.GetAll()
			if err != nil {
				return err
			}

			return c.createSecretForCluster(cluster, crt, key, csum)
		}

		return err
	}

	crt, key, csum, err := c.tls.GetAll()
	if err != nil {
		return err
	}

	// from this point on we can modify the Secret
	secret = secret.DeepCopy()

	clusterName := cluster.GetName()

	got, ok := secret.GetAnnotations()[shipperv1.SecretChecksumAnnotation]
	if !ok {
		// we got a Secret that's controlled by us (because we must've passed the
		// owner ref check to get here) but it does not have the right annotation,
		// somehow
		// this likely means that someone or something is messing with Secrets that
		// don't belong to them! stand up and protect what's rightfully ours!
		return c.updateClusterSecret(secret, clusterName, crt, key, csum)
	}

	if hex.EncodeToString(csum) != got {
		// the cert on disk has changed, update the Secret to reflect this
		glog.V(6).Infof("Expected: %v; got: %v", csum, got)
		return c.updateClusterSecret(secret, clusterName, crt, key, csum)
	}

	// all good, nothing to do
	return nil
}

func (c *Controller) createSecretForCluster(cluster *shipperv1.Cluster, crt, key, csum []byte) error {
	clusterName := cluster.GetName()
	secretName := secretNameForCluster(cluster)

	gvk := cluster.GroupVersionKind()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: c.ownNamespace,
			Annotations: map[string]string{
				shipperv1.SecretChecksumAnnotation: hex.EncodeToString(csum),
				// the convention is that Secrets should be named after their respective
				// target Clusters
				// but I don't want this to be a hard dependency so I'm putting the cluster
				// name separately in the annotations, just in case
				shipperv1.SecretClusterNameAnnotation: clusterName,
			},
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: gvk.Version,
					Kind:       gvk.Kind,
					Name:       cluster.GetName(),
					UID:        cluster.GetUID(),
				},
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       crt,
			corev1.TLSPrivateKeyKey: key,
		},
	}

	var err error
	secret, err = c.kubeClientset.CoreV1().Secrets(c.ownNamespace).Create(secret)
	if err != nil {
		return fmt.Errorf("create Secret %q for Cluster %q: %s", secretName, clusterName, err)
	}

	c.recorder.Eventf(
		secret,
		corev1.EventTypeNormal,
		reasonCreated,
		"Created Secret %q for Cluster %q",
		secret.GetName(),
		clusterName,
	)

	return err
}

func (c *Controller) updateClusterSecret(secret *corev1.Secret, clusterName string, crt, key, csum []byte) error {
	secretName := secret.GetName()

	// There may be no annotations set and `metadata.annotations` is tagged as
	// "omitempty".
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[shipperv1.SecretChecksumAnnotation] = hex.EncodeToString(csum)
	secret.Annotations[shipperv1.SecretClusterNameAnnotation] = clusterName

	secret.Data[corev1.TLSCertKey] = crt
	secret.Data[corev1.TLSPrivateKeyKey] = key

	if _, err := c.kubeClientset.CoreV1().Secrets(c.ownNamespace).Update(secret); err != nil {
		return fmt.Errorf("update Secret %q for Cluster %q: %s", secretName, clusterName, err)
	}

	// XXX do we need to update ownership info, just in case?

	c.recorder.Eventf(
		secret,
		corev1.EventTypeNormal,
		reasonUpdated,
		"Updated Secret %q for Cluster %q",
		secretName,
		clusterName,
	)

	return nil
}

func secretNameForCluster(cluster *shipperv1.Cluster) string {
	return cluster.GetName()
}
