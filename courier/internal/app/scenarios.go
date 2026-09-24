package app

import (
	"github.com/spf13/cobra"

	"github.com/JaimeStill/spike-messaging/courier/output"
	"github.com/JaimeStill/spike-messaging/courier/scenario"
)

// mountScenarios builds "scenario", with one subcommand per scenario, each
// narrating through out.
func mountScenarios(scenarios []scenario.Scenario, out *output.Output) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scenario",
		Short: "Run a narrated scenario on the broker",
		Args:  noArgs,
		// A command with no run function skips its Args check and prints its
		// help for any argument, so an unknown scenario would exit 0.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	newReporter := func() *scenario.Reporter { return scenario.NewReporter(out) }
	for _, s := range scenarios {
		cmd.AddCommand(scenario.Command(s, newReporter))
	}
	return cmd
}

// listCommand builds "list", which prints the scenario listing.
func listCommand(scenarios []scenario.Scenario) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the scenarios and what each one needs",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scenario.WriteListing(cmd.OutOrStdout(), scenarios)
			return nil
		},
	}
}
