package stand

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/image"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/env"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/project"
)

func (m *Manager) preload(ctx context.Context, state *project.State, images []string) error {
	out := m.Streams.Out

	// TODO: убрать в конфигурацию etc (ближе к созданию k3s контейнера)
	images = append(images,
		"docker.io/rancher/local-path-provisioner:v0.0.34",
		"docker.io/rancher/mirrored-coredns-coredns:1.14.1",
		"docker.io/rancher/mirrored-library-busybox:1.37.0",
		"docker.io/rancher/mirrored-metrics-server:v0.8.1",
		"docker.io/rancher/mirrored-pause:3.6",
	)

	if len(images) == 0 {
		return nil
	}

	var buf bytes.Buffer
	if err := m.Runtime.Exec(ctx, state, []string{
		"ctr", "-n", "k8s.io", "images", "ls", "-q",
	}, nil, &buf, io.Discard); err != nil {
		return fmt.Errorf("preload: list images in k3s: %w", err)
	}

	var mu sync.RWMutex
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if env.Debug {
				fmt.Fprintf(out, "preload: k3s contains: %s\n", line)
			}
			seen[line] = true
		}
	}

	canonical := func(ref string) string {
		named, err := reference.ParseAnyReference(ref)
		if err != nil {
			return ref
		}
		return named.String()
	}

	find := func(info image.InspectResponse) (string, bool) {
		mu.RLock()
		defer mu.RUnlock()

		for _, tag := range info.RepoTags {
			if tag := canonical(tag); seen[tag] {
				return tag, true
			}
		}
		for _, digest := range info.RepoDigests {
			if seen[digest] {
				return digest, true
			}
		}
		if seen[info.ID] {
			return info.ID, true
		}
		return "", false
	}

	add := func(info image.InspectResponse) {
		mu.Lock()
		defer mu.Unlock()

		for _, tag := range info.RepoTags {
			seen[canonical(tag)] = true
		}
		for _, digest := range info.RepoDigests {
			seen[digest] = true
		}
		seen[info.ID] = true
	}

	present := make([]bool, len(images))
	count := 0
	for i, ref := range images {
		if seen[ref] {
			present[i] = true
			count++
			fmt.Fprintf(out, "preload: %s found\n", ref)
			continue
		}
		if canRef := canonical(ref); seen[canRef] {
			present[i] = true
			count++
			fmt.Fprintf(out, "preload: %s found as %s\n", ref, canRef)
			continue
		}
	}

	if count == len(images) {
		return nil
	}

	var wg sync.WaitGroup
	out = iostreams.NewSafeWriter(out)

	for i, ref := range images {
		if present[i] {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			lo := iostreams.NewLineWriter(out)
			defer lo.Flush()

			out := iostreams.NewPrefixWriter(lo, fmt.Sprintf("preload[%d]: ", i))

			fmt.Fprintf(out, "pulling %s\n", ref)
			info, err := m.Runtime.Pull(ctx, ref, out)
			if err != nil {
				fmt.Fprintf(out, "pull %s: %v\n", ref, err)
				return
			}

			if asRef, ok := find(info); ok {
				present[i] = true
				add(info)
				fmt.Fprintf(out, "%s found as %s\n", ref, asRef)
				return
			}

			fmt.Fprintf(out, "importing %s\n", ref)
			if err = m.Runtime.Import(ctx, state, ref, out); err != nil {
				fmt.Fprintf(out, "import %s: %v\n", ref, err)
				return
			}

			present[i] = true
			add(info)
		}()
	}
	wg.Wait()

	failed := make([]string, 0, len(images))
	for i, ok := range present {
		if !ok {
			failed = append(failed, images[i])
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("preload: failed to load: %v", strings.Join(failed, ", "))
	}

	return nil
}
