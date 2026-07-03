// Package cmd implements the dublyobase command-line interface.
package cmd

import (
	"path/filepath"

	"github.com/dublyobase/dublyobase/core"
	"github.com/spf13/cobra"
)

var (
	flagDir  string
	flagHTTP string

	// app is constructed in PersistentPreRunE and shared by subcommands.
	app *core.App
)

// Execute builds and runs the root command.
func Execute() error {
	root := &cobra.Command{
		Use:   "dublyobase",
		Short: "All-in-one Postgres backend: provision & supervise Postgres 16/17/18, plus auth, files, SMTP, logs and an admin panel.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			s := core.DefaultSettings()
			if flagDir != "" {
				abs, err := filepath.Abs(flagDir)
				if err != nil {
					return err
				}
				s.DataDir = abs
			}
			if flagHTTP != "" {
				s.HTTPAddr = flagHTTP
			}
			app = core.NewApp(s)
			return nil
		},
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&flagDir, "dir", "./db_data",
		"data directory (Postgres clusters, storage, metadata)")
	root.PersistentFlags().StringVar(&flagHTTP, "http", "0.0.0.0:8090",
		"address to serve the API and admin UI on")

	root.AddCommand(newServeCmd(), newClusterCmd())
	return root.Execute()
}
