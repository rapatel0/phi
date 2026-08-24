package version

// Version is the current alpha release shown on the splash screen and used by
// `alpha update`. Override at build time with:
//
//	go build -ldflags="-X github.com/rapatel0/alpha/internal/version.Version=v0.2.0"
var Version = "v0.16.0"
