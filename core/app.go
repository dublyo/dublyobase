package core

import (
	"path/filepath"

	"github.com/dublyobase/dublyobase/pgsuper"
)

// App is the central object wired through the CLI, HTTP handlers and (later)
// the hook system. It owns the Postgres supervisor and instance settings.
type App struct {
	Settings   *Settings
	Supervisor *pgsuper.Supervisor
}

// NewApp constructs an App and its supervisor rooted under the data dir.
func NewApp(settings *Settings) *App {
	if settings == nil {
		settings = DefaultSettings()
	}
	return &App{
		Settings:   settings,
		Supervisor: pgsuper.New(filepath.Join(settings.DataDir, "clusters")),
	}
}
