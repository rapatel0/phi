package main

// Compiled-in extensions. Third-party Go modules register the same way:
// add a blank import here (or in a local cmd/plugins_local.go that you
// don't commit).
import (
	_ "github.com/rapatel0/alpha/internal/ext/askuser"
	_ "github.com/rapatel0/alpha/internal/ext/todo"
	_ "github.com/rapatel0/alpha/internal/ext/tokenspeed"
	_ "github.com/rapatel0/alpha/internal/ext/toolstats"
)
