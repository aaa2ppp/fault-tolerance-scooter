package kube

import (
	"context"
	_ "embed"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed valkey-bootstrap.sh
var BootstrapValkey string

const valkeyImage = "valkey/valkey:9.0.8-alpine@sha256:139d21c97b1a956e42e29b1e4ede4752a6ff52d1a0db9ef0ff4954a570f23976"

// Узел должен принимать exec до сборки кластера, когда readiness ещё не проходит.
func (c *Client) WaitValkeyRunning(ctx context.Context) error {
	return Poll(ctx, "запуск valkey-0", func(ctx context.Context) (bool, error) {
		pod, err := c.Typed.CoreV1().Pods(standNamespace).Get(ctx, "valkey-0", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning, nil
	})
}

func (c *Client) WaitValkey(ctx context.Context) error {
	return Poll(ctx, "готовность Valkey", func(ctx context.Context) (bool, error) {
		state, err := c.Typed.AppsV1().StatefulSets(standNamespace).Get(ctx, "valkey", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return state.Status.ObservedGeneration >= state.Generation && state.Status.ReadyReplicas == 6, nil
	})
}
