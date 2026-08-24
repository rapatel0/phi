package main

// Compiled-in extensions. Third-party Go modules register the same way:
// add a blank import here (or in a local cmd/plugins_local.go that you
// don't commit).
import (
	_ "github.com/pulseaiclub/phi/internal/ext/askuser"
	_ "github.com/pulseaiclub/phi/internal/ext/todo"
	_ "github.com/pulseaiclub/phi/internal/ext/tokenspeed"
)
