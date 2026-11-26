package kubectl

import (
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

// NewCmdValkey запускает valkey-cli внутри кластера, где доступны адреса всех шардов.
func NewCmdValkey(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "valkey COMMAND [ARG...]",
		Short:   "Выполнить команду valkey-cli с поддержкой Redis Cluster",
		Example: "  scooterctl valkey CLUSTER INFO\n  scooterctl valkey TTL 'user:{UUID}:discount'",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := NewKubectlOptions(streams)
			command := []string{"-n", "scooterflow", "exec", "service/valkey", "--", "valkey-cli", "-c", "-e", "--raw"}
			if err := o.Complete(f, append(command, args...)); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
}
