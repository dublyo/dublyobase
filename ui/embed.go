// Package ui embeds the compiled admin SPA into the binary. In later phases
// `dist/` is produced by the Vite build; for now it holds a placeholder shell.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distDir embed.FS

// DistFS returns the embedded admin UI as a filesystem rooted at dist/.
func DistFS() fs.FS {
	sub, err := fs.Sub(distDir, "dist")
	if err != nil {
		panic(err) // dist/ is embedded at build time; this is a programmer error
	}
	return sub
}
