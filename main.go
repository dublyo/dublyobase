// Command dublyobase is an open-source, self-hostable Postgres backend:
// it connects to an external Postgres via DATABASE_URL and layers auth,
// sessions, file storage, SMTP, logs and an admin panel on top — the
// PocketBase developer experience, grounded in real Postgres.
package main

import (
	"fmt"
	"os"

	"github.com/dublyo/dublyobase/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
