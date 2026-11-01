package stand

import (
	"context"
	"fmt"
	"time"

	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/kube"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/project"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (m *Manager) Up(ctx context.Context, services []config.Service) (err error) {
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err = m.Runtime.Check(ctx); err != nil {
		return err
	}
	state, err := m.load(true)
	if err != nil {
		return err
	}
	if state.Reset != nil {
		return fmt.Errorf("сброс данных не завершён; выполните scooterctl reset --yes")
	}
	if err = m.compatible(state); err != nil {
		return err
	}
	defer func() {
		if err != nil && ctx.Err() == nil {
			diagnosticCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			fmt.Fprintln(m.Streams.ErrOut, "Startup failed. Diagnostics:")
			_ = m.Runtime.Logs(diagnosticCtx, state, m.Streams.ErrOut)
			if client, e := kube.New(m.Config.Kubeconfig()); e == nil {
				client.Diagnostics(diagnosticCtx, services, m.Streams.ErrOut)
			}
		}
	}()
	state.Bindings = m.Config.PortBindings()
	if err = m.Store.Save(state); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Starting one k3s node")
	if err = m.Runtime.Ensure(ctx, state, m.Streams.Out); err != nil {
		return err
	}
	data, err := m.Runtime.Kubeconfig(ctx, state)
	if err != nil {
		return err
	}
	if err = kube.WriteConfig(data, m.Config.Kubeconfig(), state.Runtime.Ports.API); err != nil {
		return err
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	if err = client.WaitNode(ctx); err != nil {
		return err
	}
	if err = m.build(ctx, state, services); err != nil {
		return err
	}
	if err = m.deploy(ctx, state, services, client); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Стенд запущен; сервисы готовы")
	m.addresses()
	fmt.Fprintln(m.Streams.Out, "Kafka UI: http://localhost:18090\nБД и etcd опубликованы локально: scooterctl access")
	return nil
}

func (m *Manager) addresses() {
	for _, service := range m.Config.Services {
		if service.GRPC.Host != 0 {
			fmt.Fprintf(
				m.Streams.Out,
				"%s: gRPC 127.0.0.1:%d, admin http://127.0.0.1:%d/healthcheck/live\n",
				service.Name,
				service.GRPC.Host,
				service.Admin.Host,
			)
		}
	}
	p := m.Config.Runtime.Ports
	fmt.Fprintf(
		m.Streams.Out,
		"\nHTTP:    http://localhost:%d/v1/echo\nSwagger: http://localhost:%d/docs/\ngRPC:    127.0.0.1:%d\nAdmin:   http://localhost:%d/healthcheck/live\n",
		p.HTTP,
		p.HTTP,
		p.GRPC,
		p.Admin,
	)
}

func (m *Manager) build(ctx context.Context, state *project.State, services []config.Service) error {
	for _, service := range services {
		fmt.Fprintf(m.Streams.Out, "Building %s\n", service.Name)
		image, err := m.Runtime.Build(ctx, m.Config.Root, service, m.Streams.Out)
		if err != nil {
			return err
		}
		state.Images[service.Name] = image
		if err = m.Store.Save(state); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) deploy(
	ctx context.Context,
	state *project.State,
	services []config.Service,
	client *kube.Client,
) error {
	if err := m.validateManifests(state, services); err != nil {
		return err
	}
	envoy, err := kube.RenderEnvoy(m.Config)
	if err != nil {
		return err
	}
	if err := m.deployEnvoy(ctx, envoy, client); err != nil {
		return err
	}
	if kube.NeedsDependencies(services) {
		resources, err := kube.RenderDependencies(m.Config.Root)
		if err != nil {
			return err
		}
		fmt.Fprintln(m.Streams.Out, "Запуск PostgreSQL, Kafka и отдельного config-etcd")
		if err := m.deployDependencies(ctx, state, resources, client); err != nil {
			return err
		}
		fmt.Fprintln(m.Streams.Out, "Применение миграций и учебных данных")
		if err := m.applyMigrations(ctx, resources, client); err != nil {
			return err
		}
	}
	fmt.Fprintln(m.Streams.Out, "Деплой сервисов")
	if err := m.deployServices(ctx, state, services, client); err != nil {
		return err
	}
	return nil
}

// Validate/render all selected manifests before mutating Kubernetes resources.
func (m *Manager) validateManifests(state *project.State, services []config.Service) error {
	for _, service := range services {
		image := state.Images[service.Name]
		if image == "" {
			return fmt.Errorf("%s has no built image; run scooterctl build %s", service.Name, service.Name)
		}
		if _, err := kube.Render(m.Config.Root, service, image); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) deployEnvoy(
	ctx context.Context,
	envoy []*unstructured.Unstructured,
	client *kube.Client,
) error {
	if err := client.Apply(ctx, envoy, m.Streams.Out); err != nil {
		return err
	}
	service := kube.EnvoyService()
	start := time.Now()
	if err := client.WaitDeployment(ctx, service); err != nil {
		return err
	}
	fmt.Fprintf(m.Streams.Out, "deployment/%s ready after %v\n", service.Deployment, time.Since(start).Round(time.Second))
	return nil
}

func (m *Manager) ensureValkeyInitialized(
	ctx context.Context,
	state *project.State,
	client *kube.Client,
) error {
	if err := client.WaitValkeyRunning(ctx); err != nil {
		return err
	}
	bootstrap := []string{"kubectl", "-n", "scooterflow", "exec", "valkey-0", "--", "sh", "-ec", kube.BootstrapValkey}
	if err := m.Runtime.Exec(ctx, state, bootstrap, nil, m.Streams.Out, m.Streams.ErrOut); err != nil {
		return fmt.Errorf("инициализация Valkey: %w", err)
	}
	if err := client.WaitValkey(ctx); err != nil {
		return err
	}
	return nil
}

func (m *Manager) deployDependencies(
	ctx context.Context,
	state *project.State,
	resources kube.DependencyResources,
	client *kube.Client,
) error {
	if err := client.Apply(ctx, resources.Infrastructure, m.Streams.Out); err != nil {
		return err
	}

	if err := client.WaitDeployments(ctx, "scooterflow", []string{
		"rental-postgres",
		"pricing-postgres",
		"profile-postgres",
		"scooter-postgres",
		"rental-kafka",
		"config-etcd",
		"akhq",
	}, m.Streams.Out); err != nil {
		return err
	}
	m.ensureValkeyInitialized(ctx, state, client)
	return nil
}

func (m *Manager) applyMigrations(
	ctx context.Context,
	resources kube.DependencyResources,
	client *kube.Client,
) error {
	if err := client.Apply(ctx, resources.Migrations, m.Streams.Out); err != nil {
		return err
	}
	start := time.Now()
	if err := client.WaitJob(ctx, "scooterflow", resources.JobName); err != nil {
		return err
	}
	fmt.Fprintf(m.Streams.Out, "job/%s done in %v\n", resources.JobName, time.Since(start).Round(time.Second))
	return nil
}

func (m *Manager) deployServices(
	ctx context.Context,
	state *project.State,
	services []config.Service,
	client *kube.Client,
) error {
	for _, service := range services {
		image := state.Images[service.Name]
		fmt.Fprintf(m.Streams.Out, "Deploying %s (%s)\n", service.Name, image)
		if err := m.Runtime.Import(ctx, state, image, m.Streams.Out); err != nil {
			return err
		}
		objects, err := kube.Render(m.Config.Root, service, image)
		if err != nil {
			return err
		}
		if err = client.Apply(ctx, objects, m.Streams.Out); err != nil {
			return err
		}
		start := time.Now()
		if err = client.WaitDeployment(ctx, service); err != nil {
			return err
		}
		fmt.Fprintf(m.Streams.Out, "deployment/%s ready after %v\n", service.Deployment, time.Since(start).Round(time.Second))
	}
	return nil
}
