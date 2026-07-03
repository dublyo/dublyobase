// Command dublyobase is an all-in-one, self-hostable Postgres backend:
// it provisions and supervises Postgres 16/17/18, and (over time) layers
// auth, sessions, file storage, SMTP, logs and an admin panel on top —
// the PocketBase developer experience, grounded in real Postgres.
package main

import (
	"fmt"
	"os"

	"github.com/dublyobase/dublyobase/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
