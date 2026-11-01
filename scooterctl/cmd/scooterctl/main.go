package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/env"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/pkg/cmd"
)

var version = "dev"
var needTimestamp = env.OnEnv("TIMESTAMP")

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errOut, flush := configureOutput(os.Stderr)
	defer flush()

	out, flush := configureOutput(os.Stdout)
	defer flush()

	if needTimestamp {
		const layout = "2006-01-02 15:04:05 "
		out = iostreams.NewTimestampWriter(out, layout)
	}

	command := cmd.NewDefaultScooterctlCommand(iostreams.IOStreams{
		In:     os.Stdin,
		Out:    out,
		ErrOut: errOut,
	}, version)
	if err := command.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func configureOutput(out io.Writer) (_ io.Writer, flush func() error) {
	lw := iostreams.NewLineWriter(iostreams.NewSafeWriter(out))
	return lw, lw.Flush
}
