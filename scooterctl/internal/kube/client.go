package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/project"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	Typed   kubernetes.Interface
	Dynamic dynamic.Interface
	REST    *rest.Config
}

func WriteConfig(data []byte, path string, port int) error {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return err
	}
	if len(cfg.Clusters) != 1 {
		return fmt.Errorf("expected a single cluster in k3s kubeconfig")
	}
	for _, cluster := range cfg.Clusters {
		cluster.Server = "https://127.0.0.1:" + strconv.Itoa(port)
	}
	raw, err := clientcmd.Write(*cfg)
	if err != nil {
		return err
	}
	return project.AtomicWrite(path, raw)
}

func New(path string) (*Client, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, fmt.Errorf("read course kubeconfig; run scooterctl up: %w", err)
	}
	cfg.Timeout = 10 * time.Second
	cfg.UserAgent = "scooterctl"
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{
		Typed:   typed,
		Dynamic: dyn,
		REST:    cfg,
	}, nil
}

type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

func Poll(ctx context.Context, description string, check func(context.Context) (bool, error)) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last error
	for {
		ok, err := check(ctx)
		if ok && err == nil {
			return nil
		}
		if _, ok := errors.AsType[*fatalError](err); ok {
			return fmt.Errorf("%s: %w", description, err)
		}
		last = err
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("%s: %w (%v)", description, ctx.Err(), last)
			}
			return fmt.Errorf("%s: %w", description, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *Client) WaitNode(ctx context.Context) error {
	return Poll(ctx, "waiting for the k3s node", func(ctx context.Context) (bool, error) {
		nodes, err := c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		if len(nodes.Items) != 1 {
			return false, fmt.Errorf("expected one node, got %d", len(nodes.Items))
		}
		for _, condition := range nodes.Items[0].Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func (c *Client) WaitDeployment(ctx context.Context, s config.Service) error {
	return Poll(ctx, "waiting for "+s.Name, func(ctx context.Context) (bool, error) {
		d, err := c.Typed.AppsV1().Deployments(s.Namespace).Get(ctx, s.Deployment, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		for _, condition := range d.Status.Conditions {
			if condition.Reason == "ProgressDeadlineExceeded" {
				return false, &fatalError{"deployment exceeded its progress deadline: " + condition.Message}
			}
		}
		if d.Status.ObservedGeneration < d.Generation ||
			d.Status.UpdatedReplicas != desired ||
			d.Status.AvailableReplicas != desired ||
			d.Status.Replicas != desired {
			return false, nil
		}
		// Deployment counters may already exclude terminating old Pods.
		// Wait for their shutdown before a fresh demo/load connects to the service.
		pods, err := c.Typed.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(d.Spec.Selector),
		})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				return false, nil
			}
		}
		return true, nil
	})
}

func (c *Client) WaitDeployments(ctx context.Context, namespace string, deploymentNames []string, out io.Writer) error {
	if len(deploymentNames) == 0 {
		return nil
	}

	start := time.Now()
	pending := make(map[string]bool, len(deploymentNames))
	for _, name := range deploymentNames {
		pending[name] = true
	}

	return Poll(ctx, "waiting for "+strings.Join(deploymentNames, ", "), func(ctx context.Context) (bool, error) {
		dl, err := c.Typed.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}

		seen := make(map[string]bool, len(dl.Items))
		for _, d := range dl.Items {
			if !pending[d.Name] {
				continue
			}
			seen[d.Name] = true

			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}

			for _, condition := range d.Status.Conditions {
				if condition.Reason == "ProgressDeadlineExceeded" {
					return false, &fatalError{fmt.Sprintf("deployment %s exceeded its progress deadline: %s", d.Name, condition.Message)}
				}
			}

			if d.Status.ObservedGeneration >= d.Generation &&
				d.Status.UpdatedReplicas == desired &&
				d.Status.AvailableReplicas == desired &&
				d.Status.Replicas == desired {
				if out != nil {
					fmt.Fprintf(out, "deployment/%s ready after %v\n", d.Name, time.Since(start).Round(time.Second))
				}
				delete(pending, d.Name)
			}
		}

		for name := range pending {
			if !seen[name] && time.Since(start) > 5*time.Second {
				return false, &fatalError{fmt.Sprintf("deployment %s/%s not found after 5s", namespace, name)}
			}
		}

		return len(pending) == 0, nil
	})
}
