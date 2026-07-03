package core

// Settings holds the runtime configuration for a dublyobase instance.
// (In later milestones this is persisted as an encrypted JSON blob in the
// _dbo control schema; for now it is process-level configuration.)
type Settings struct {
	AppName  string
	AppURL   string
	DataDir  string // root data dir: clusters/, storage/, meta/, backups/
	HTTPAddr string
}

// DefaultSettings returns sane defaults for local development.
func DefaultSettings() *Settings {
	return &Settings{
		AppName:  "dublyobase",
		AppURL:   "http://localhost:8090",
		DataDir:  "./db_data",
		HTTPAddr: "0.0.0.0:8090",
	}
}
