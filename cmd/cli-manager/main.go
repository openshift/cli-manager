package main

import (
	"os"

	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	cli_manager "github.com/openshift/cli-manager/pkg/cmd/cli-manager"
)

func main() {
	command := NewCLIManagerCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewCLIManagerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cli-manager",
		Short: "Command for delivering CLI tools",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	start := cli_manager.NewCLIManagerCommand("start", false)
	cmd.AddCommand(start)

	return cmd
}
