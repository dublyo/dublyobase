// Package cmd implements the dublyobase command-line interface. Configuration
// comes entirely from environment variables (see core.Config) — there are no
// config flags, matching the PaaS template contract.
package cmd

import (
	"fmt"

	"github.com/dublyo/dublyobase/core"
	"github.com/spf13/cobra"
)

// Execute builds and runs the root command.
func Execute() error {
	root := &cobra.Command{
		Use:          "dublyobase",
		Short:        "Postgres-backed BaaS — auth, realtime, storage and an admin UI in one binary.",
		SilenceUsage: true,
	}

	root.AddCommand(newServeCmd())
	root.AddCommand(newAdminCmd())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version and exit",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("dublyobase", core.Version)
		},
	})

	return root.Execute()
}
