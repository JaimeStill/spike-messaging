package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/standards-lab/go-core/process"

	"github.com/JaimeStill/spike-messaging/output"
	"github.com/JaimeStill/spike-messaging/scenario"
)

// App is the application: the command tree assembled over the
// infrastructure, and the output every command renders through.
type App struct {
	root *cobra.Command
	out  *output.Output
}

// New is the cold start: it composes the layers in dependency order and
// performs no I/O. cobra's own output (help, usage) goes through the root's
// writers, and every command's result through the one output.Output built
// over the same two streams.
func New(stdout, stderr io.Writer) *App {
	cfg := &Config{}
	out := output.New(stdout, stderr)
	infra := newInfrastructure(cfg)
	scenarios := scenario.Scenarios(infra.Broker, infra.Needs())

	root := newRoot(cfg, infra, scenarios)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(commands(scenarios, out)...)

	return &App{root: root, out: out}
}

// Run is the hot start: it executes the tree under ctx, which cancels the
// running command when the process is signalled. The tree silences cobra's
// own reporting, so the error a command returns is rendered here, once, and
// sets the exit code: a usage error exits with process.ExitUsage, any other
// with process.ExitFailure.
func (a *App) Run(ctx context.Context) int {
	err := a.root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return process.ExitOK
	case errors.Is(err, scenario.ErrUsage):
		a.out.Error(err)
		return process.ExitUsage
	default:
		a.out.Error(err)
		return process.ExitFailure
	}
}

// SetArgs replaces the process arguments the tree parses, for tests.
func (a *App) SetArgs(args []string) { a.root.SetArgs(args) }

// usage marks err as the invocation's fault.
func usage(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", scenario.ErrUsage, err)
}

// noArgs is cobra.NoArgs with its error marked as a usage error, so an
// unknown subcommand exits with the usage code.
func noArgs(cmd *cobra.Command, args []string) error {
	return usage(cobra.NoArgs(cmd, args))
}

// newRoot builds the root command with cfg's persistent flags bound. Before
// any subcommand runs, it fails when the flags name a broker courier cannot
// build. Run without a subcommand, it prints its help and the scenario
// listing.
func newRoot(cfg *Config, infra *Infrastructure, scenarios []scenario.Scenario) *cobra.Command {
	root := &cobra.Command{
		Use:   "courier",
		Short: "Run the event and reactor layer's scenarios on a broker",
		Long: "courier runs narrated scenarios that each show one capability of the spike's event\n" +
			"and reactor layer on a broker: an interval reactor, a delivery group, retry, a\n" +
			"permanent failure, and the drain.",
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return usage(infra.Validate())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Help(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Scenarios:")
			scenario.WriteListing(cmd.OutOrStdout(), scenarios)
			return nil
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage(err) })
	cfg.bind(root.PersistentFlags())
	return root
}
