package util

import (
	"encoding/base64"
	"fmt"
	"k8s.io/api/core/v1"
	"strconv"
)

type KeyNotFoundError struct {
	Key string
}

func (e *KeyNotFoundError) Error() string {
	return fmt.Sprintf("key %q doesn't exist", e.Key)
}

func IsKeyNotFoundError(err error) bool {
	_, ok := err.(*KeyNotFoundError)
	return ok
}

type DecodeError struct {
	Key     string
	Message string
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("could not decode data from %q: %s", e.Key, e.Message)
}

func IsDecodeError(err error) bool {
	_, ok := err.(*DecodeError)
	return ok
}

func GetBool(secret *v1.Secret, k string) (bool, error) {
	if v, ok := secret.Data[k]; ok {
		if decoded, err := base64.StdEncoding.DecodeString(string(v)); err != nil {
			return false, &DecodeError{Key: k, Message: err.Error()}
		} else if insecure, err := strconv.ParseBool(string(decoded)); err != nil {
			return false, &DecodeError{Key: k, Message: err.Error()}
		} else {
			return insecure, nil
		}
	}

	return false, &KeyNotFoundError{Key: k}
}
