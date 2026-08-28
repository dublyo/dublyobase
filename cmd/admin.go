package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/dublyo/dublyobase/core"
	"github.com/spf13/cobra"
)

// newAdminCmd provides the recovery path an operator otherwise has to improvise
// with psql. Forgetting the admin password used to mean hand-editing a bcrypt
// hash into the database — an operation with no audit trail, done under
// pressure, on production.
func newAdminCmd() *cobra.Command {
	admin := &cobra.Command{
		Use:   "admin",
		Short: "Administrative recovery operations",
	}

	var email, password string
	reset := &cobra.Command{
		Use:   "reset-password",
		Short: "Reset an admin password and revoke that admin's sessions",
		Long: "Reset an admin password without the old one.\n\n" +
			"Runs from the CLI rather than over HTTP on purpose: recovery should require\n" +
			"proof of control over the deployment, not over an inbox. Every use is written\n" +
			"to the audit log, and all of that admin's sessions are revoked.\n\n" +
			"Omit --password to have one generated and printed once.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := core.LoadConfig()
			if err != nil {
				return err
			}
			log := core.NewLogger(cfg)
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			pool, err := core.Connect(ctx, cfg, log)
			if err != nil {
				return err
			}
			defer pool.Close()

			generated := false
			if password == "" {
				password, err = generatePassword()
				if err != nil {
					return err
				}
				generated = true
			}
			admin, err := core.ResetAdminPasswordByEmail(ctx, pool, email, password, cfg.BcryptCost)
			if err != nil {
				return err
			}
			fmt.Printf("password reset for %s\n", admin.Email)
			if generated {
				fmt.Printf("temporary password: %s\n", password)
				fmt.Println("copy it now — it is not stored and will not be shown again")
			}
			fmt.Println("all sessions for this admin were revoked; a password change is required at next login")
			return nil
		},
	}
	reset.Flags().StringVar(&email, "email", "", "email of the admin to reset (required)")
	reset.Flags().StringVar(&password, "password", "", "new password; generated if omitted")
	_ = reset.MarkFlagRequired("email")

	admin.AddCommand(reset)
	return admin
}

func generatePassword() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "dbo-" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
