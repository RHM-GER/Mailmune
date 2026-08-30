package secrets

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const serviceName = "InboxSentinel"

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Set(reference, secret string) error
	Get(reference string) (string, error)
	Delete(reference string) error
}

type KeyringStore struct{}

func NewKeyringStore() *KeyringStore { return &KeyringStore{} }

func (s *KeyringStore) Set(reference, secret string) error {
	if reference == "" || secret == "" {
		return errors.New("secret reference and value are required")
	}
	return keyring.Set(serviceName, reference, secret)
}

func (s *KeyringStore) Get(reference string) (string, error) {
	value, err := keyring.Get(serviceName, reference)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read OS keyring: %w", err)
	}
	return value, nil
}

func (s *KeyringStore) Delete(reference string) error {
	err := keyring.Delete(serviceName, reference)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
