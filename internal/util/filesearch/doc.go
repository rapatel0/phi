// Package filesearch finds workspace files by walking the tree in process.
//
// The walk honors .gitignore, so a caller never offers a build artifact, and
// it stops as soon as it has enough matches. It needs no external binary and
// no cgo, which keeps the release cross-compiling to every target.
//
// ResolveFD stays for callers that still shell out to fd for glob search.
package filesearch
