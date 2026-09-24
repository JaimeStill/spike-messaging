package app

import (
	"github.com/spf13/cobra"

	"github.com/JaimeStill/spike-messaging/output"
	"github.com/JaimeStill/spike-messaging/scenario"
)

// commands is the list of mounts: the scenario mount and listing from
// scenarios.go. This file composes and does nothing else.
func commands(scenarios []scenario.Scenario, out *output.Output) []*cobra.Command {
	return []*cobra.Command{mountScenarios(scenarios, out), listCommand(scenarios)}
}
