// Package state persists the last commit git-monitor already reported, so a
// pod restart doesn't fire a duplicate webhook. It uses a single Kubernetes
// Secret instead of a database.
package state

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// lastCommitKey is the Secret data field holding the last commit reported.
const lastCommitKey = "lastCommit"

// Store reads and writes the last reported commit to a named Secret.
type Store struct {
	client    kubernetes.Interface
	namespace string
	name      string
}

// New returns a Store backed by the given Secret, creating it (empty) if it
// doesn't already exist.
func New(ctx context.Context, client kubernetes.Interface, namespace, name string) (*Store, error) {
	s := &Store{client: client, namespace: namespace, name: name}

	_, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Data: map[string][]byte{},
		}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("creating state secret %s/%s: %w", namespace, name, err)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state secret %s/%s: %w", namespace, name, err)
	}
	return s, nil
}

// LastCommit returns the last commit SHA that was reported, or "" if none
// has been reported yet.
func (s *Store) LastCommit(ctx context.Context) (string, error) {
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading state secret %s/%s: %w", s.namespace, s.name, err)
	}
	return string(secret.Data[lastCommitKey]), nil
}

// SetLastCommit records sha as the last commit reported.
func (s *Store) SetLastCommit(ctx context.Context, sha string) error {
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("reading state secret %s/%s: %w", s.namespace, s.name, err)
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[lastCommitKey] = []byte(sha)

	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating state secret %s/%s: %w", s.namespace, s.name, err)
	}
	return nil
}
