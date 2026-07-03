package cmd

import (
	"fmt"

	"github.com/dublyobase/dublyobase/pgsuper"
	"github.com/spf13/cobra"
)

func newClusterCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cluster",
		Short: "Manage supervised Postgres clusters (16/17/18)",
	}

	ensure := &cobra.Command{
		Use:   "ensure <version>",
		Short: "Initialize (initdb) and start the cluster for a major version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := pgsuper.ParseVersion(args[0])
			if err != nil {
				return err
			}
			cl, err := app.Supervisor.EnsureCluster(v)
			if err != nil {
				return err
			}
			fmt.Printf("cluster pg%s ready on port %d (data: %s)\n", v, cl.Port, cl.DataDir)
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List supported versions and detect their binaries",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, v := range pgsuper.SupportedVersions {
				status := "found"
				if _, err := v.BinDir(); err != nil {
					status = "missing (" + err.Error() + ")"
				}
				fmt.Printf("pg%-3s port %d  binaries: %s\n", v, v.Port(), status)
			}
			return nil
		},
	}

	provision := &cobra.Command{
		Use:   "provision <version> <project>",
		Short: "Provision a project database inside the given-version cluster",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := pgsuper.ParseVersion(args[0])
			if err != nil {
				return err
			}
			cl, err := app.Supervisor.ProvisionProject(v, args[1])
			if err != nil {
				return err
			}
			fmt.Printf("provisioned %q on pg%s\n  conn: %s\n", args[1], v, cl.ConnString(args[1]))
			return nil
		},
	}

	c.AddCommand(ensure, list, provision)
	return c
}
