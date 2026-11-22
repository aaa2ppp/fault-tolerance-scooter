package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/env"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/progress"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/project"
	"github.com/moby/buildkit/session"
	archive "github.com/moby/go-archive"
	"github.com/moby/patternmatcher/ignorefile"
)

var builderVer = build.BuilderBuildKit

func init() {
	if !env.OnEnv("DOCKER_BUILDKIT", true) {
		builderVer = build.BuilderV1
	}
}

func (e *Engine) Build(ctx context.Context, root string, s config.Service, out io.Writer) (string, error) {
	dir := filepath.Join(root, s.Context)
	excludes := []string{}
	ignore, err := os.Open(filepath.Join(dir, ".dockerignore"))
	if err == nil {
		excludes, err = ignorefile.ReadAll(ignore)
		ignore.Close()
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	archiveReader, err := archive.TarWithOptions(dir, &archive.TarOptions{
		ExcludePatterns: excludes,
	})
	if err != nil {
		return "", err
	}
	defer archiveReader.Close()

	tag := s.Image + ":local"
	opts := build.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: filepath.ToSlash(s.Dockerfile),
		Remove:     true,
		Version:    builderVer,
	}

	// Сессия BuildKit (обязательна для Windows/macOS).
	buildCtx, cancelBuild := context.WithCancel(ctx)

	var sessDone chan struct{}
	defer func() {
		cancelBuild()
		if sessDone != nil {
			<-sessDone
		}
	}()

	if builderVer == build.BuilderBuildKit {
		sess, err := session.NewSession(buildCtx, "scooterctl")
		if err != nil {
			return "", fmt.Errorf("create buildkit session: %w", err)
		}

		sessDone = make(chan struct{}, 1)
		go func() {
			defer close(sessDone)
			_ = sess.Run(buildCtx, func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
				return e.client.DialHijack(ctx, "/session", proto, meta)
			})
		}()
		opts.SessionID = sess.ID()
	}

	result, err := e.client.ImageBuild(buildCtx, archiveReader, opts)
	if err != nil {
		hint := ""
		if opts.Version == build.BuilderBuildKit {
			hint = " (hint: set DOCKER_BUILDKIT=0 to use the legacy builder)"
		}
		return "", fmt.Errorf("build %s (builder=%s): %w%s", s.Name, opts.Version, err, hint)
	}
	defer result.Body.Close()

	if err := consumeProgress(result.Body, out); err != nil {
		return "", err
	}

	info, err := e.client.ImageInspect(ctx, tag)
	if err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(info.ID, "sha256:")
	if len(digest) < 16 {
		return "", fmt.Errorf("unexpected image ID %q", info.ID)
	}
	immutable := s.Image + ":sha-" + digest[:16]
	return immutable, e.client.ImageTag(ctx, tag, immutable)
}

// for dev only
var rawProgress = env.OnEnv("RAW_BUILD_PROGRESS")

func consumeProgressRaw(reader io.Reader, out io.Writer) error {
	_, _ = io.Copy(out, reader)
	return nil
}

// consumeProgress renders build progress from reader to out, transparently
// handling both BuilderV1 JSON streams and BuildKit trace envelopes.
func consumeProgress(reader io.Reader, out io.Writer) error {
	if rawProgress {
		return consumeProgressRaw(reader, out)
	}
	var lops slog.HandlerOptions
	if env.Debug {
		lops.Level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &lops))
	r := progress.NewTraceRender(out, logger)
	return jsonmessage.DisplayJSONMessagesStream(reader, out, 0, false, r.AuxCallback)
}

func (e *Engine) Import(ctx context.Context, s *project.State, imageName string, out io.Writer) error {
	cmd := []string{"ctr", "-n", "k8s.io", "images", "import"}

	if named, err := reference.ParseAnyReference(imageName); err == nil {
		if _, digested := named.(reference.Digested); digested {
			if n, ok := named.(reference.Named); ok {
				cmd = append(cmd, "-digests", "-base-name", n.Name())
			}
		}
	}

	cmd = append(cmd, "-")

	saveStream, err := e.client.ImageSave(ctx, []string{imageName})
	if err != nil {
		return err
	}
	defer saveStream.Close()

	out = iostreams.NewSafeWriter(out)
	lo := iostreams.NewLineWriter(out)
	le := iostreams.NewLineWriter(out)
	defer func() {
		_ = lo.Flush()
		_ = le.Flush()
	}()

	return e.Exec(ctx, s, cmd, saveStream,
		iostreams.NewPrefixWriter(lo, "ctr[1]: "),
		iostreams.NewPrefixWriter(le, "ctr[2]: "),
	)
}

func (e *Engine) Exec(
	ctx context.Context,
	s *project.State,
	args []string,
	in io.Reader,
	out, errOut io.Writer,
) error {
	result, err := e.client.ContainerExecCreate(
		ctx,
		ContainerName(s),
		container.ExecOptions{
			Cmd:          args,
			AttachStdin:  in != nil,
			AttachStdout: true,
			AttachStderr: true,
		},
	)
	if err != nil {
		return err
	}
	attach, err := e.client.ContainerExecAttach(ctx, result.ID, container.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attach.Close()
	var stdinErr <-chan error
	if in != nil {
		ch := make(chan error, 1)
		stdinErr = ch
		go func() {
			_, err := io.Copy(attach.Conn, in)
			if err != nil {
				ch <- err
			}
			_ = attach.CloseWrite()
		}()
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			attach.Close()
		case <-done:
		}
	}()
	if _, err = stdcopy.StdCopy(out, errOut, attach.Reader); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, err := e.client.ContainerExecInspect(ctx, result.ID)
	if err != nil {
		return err
	}
	if status.ExitCode != 0 {
		if stdinErr != nil {
			select {
			case e := <-stdinErr:
				return fmt.Errorf("%s exited with code %d: stdin: %v", args[0], status.ExitCode, e)
			default:
			}
		}
		return fmt.Errorf("%s exited with code %d", args[0], status.ExitCode)
	}
	return nil
}

func demultiplex(ctx context.Context, reader io.ReadCloser, out, errOut io.Writer) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			reader.Close()
		case <-done:
		}
	}()
	_, err := stdcopy.StdCopy(out, errOut, reader)
	return err
}

func (e *Engine) Pull(ctx context.Context, ref string, out io.Writer) (image.InspectResponse, error) {
	info, err := e.client.ImageInspect(ctx, ref)
	if err != nil && !errdefs.IsNotFound(err) {
		return image.InspectResponse{}, err
	}
	if errdefs.IsNotFound(err) {
		fmt.Fprintf(out, "Downloading %s\n", ref)
		stream, err := e.client.ImagePull(ctx, ref, image.PullOptions{})
		if err != nil {
			return image.InspectResponse{}, err
		}
		err = consumeProgress(stream, out)
		stream.Close()
		if err != nil {
			return image.InspectResponse{}, err
		}
		if info, err = e.client.ImageInspect(ctx, ref); err != nil {
			return image.InspectResponse{}, err
		}
	}
	return info, nil
}
