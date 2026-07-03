package core

// Version is the dublyobase release version, surfaced by /health and the
// `version` command. It is overridden at build time by the release pipeline:
//
//	go build -ldflags "-X github.com/dublyo/dublyobase/core.Version=v0.5.0"
var Version = "dev"
