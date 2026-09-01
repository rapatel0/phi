// Package wasmhost loads WASM plugins into the extension host.
//
// The runtime is wazero (pure Go, no CGO). Guests may be written in anything
// that emits wasm32; Go plugins use GOOS=wasip1 GOARCH=wasm. Filesystem access
// is not provided. Commands register through the alpha host import module.
package wasmhost
