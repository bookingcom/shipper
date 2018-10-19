package configurator

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

type SecretNotPopulatedError struct {
	ServiceAccount *corev1.ServiceAccount
}

func (s SecretNotPopulatedError) Error() string {
	return fmt.Sprintf("There is no secret for the %s service account", s.ServiceAccount.Name)
}

func NewSecretNotPopulatedError(serviceAccount *corev1.ServiceAccount) *SecretNotPopulatedError {
	return &SecretNotPopulatedError{ServiceAccount: serviceAccount}
}

func IsSecretNotPopulatedError(err error) bool {
	_, ok := err.(SecretNotPopulatedError)

	return ok
}
