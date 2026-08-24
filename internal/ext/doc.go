// Package ext is Phi's in-process extension host.
//
// Go cannot safely hot-load arbitrary .so plugins. Extensions register at
// init() via Register, and cmd blank-imports the packages it wants:
//
//	import _ "github.com/pulseaiclub/phi/internal/ext/todo"
//
// Third-party modules do the same from a local cmd/plugins.go.
package ext
