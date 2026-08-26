# lens

`internal/ext/lens` tells the model about problems in a file right after it
writes one, so a mistake is corrected in the same turn instead of at the next
build.

## What it does

After a successful `edit` or `write`, the extension runs the checker for that
file type and appends the findings to the tool result the model reads:

```text
edit(internal/media/format.go)
  -> ok, 1 replacement
  -> lens: 1 problem
       internal/media/format.go:77:27: cannot use Accepts("xai", "image/gif")
         (value of type bool) as int value in variable declaration
```

A clean file produces no note. A note after every edit trains the model to skip
the section, which is where the real findings appear.

## Checkers

| Extension | Command | Requires |
| --- | --- | --- |
| `.go` | `go vet ./pkg` | the Go toolchain |
| `.py` | `ruff check --output-format=concise` | `ruff` on PATH |
| `.sh`, `.bash` | `shellcheck -f gcc` | `shellcheck` on PATH |
| `.rs` | `cargo check --quiet` | `cargo` and `Cargo.toml` |
| `.ts`, `.tsx` | `tsc --noEmit` | `tsc` and `tsconfig.json` |

A checker whose binary is missing is skipped without a message. The agent must
not be told about a tool the project does not use.

The table is fixed rather than configurable. A config file would be one more
surface to document and version, and the mapping from a language to its usual
checker is not controversial.

### Why `go vet` and not `gopls`

`gopls` is the Go-native language server and reports the same type errors, plus
style suggestions. It takes about 1.6 s per file even warm, against 0.18 s for
`go vet`. A hook that runs after every edit cannot spend 1.6 s.

`go vet` type-checks the whole package, so it finds undefined symbols and
signature drift that a single-file parse cannot. Measured against injected
faults, it catches type mismatches, undefined symbols, wrong return arity, and
unused variables.

## Output

Every checker here prints `file:line:col: message`, so one parser covers all of
them. `go vet` adds a `vet:` prefix and a `# package` header, which are
stripped.

A package-wide checker reports every file in the package. Only the file just
written is reported, or the model is told about problems it did not cause and
cannot see.

At most 10 problems are reported. A file with more is usually mid-refactor, and
a wall of findings buries the first one, which is normally the cause of the
rest.

## Limits

A checker is bounded at 10 seconds. A checker that is missing, misconfigured,
or slow yields nothing rather than an error: this runs after an edit that
already succeeded, so a broken checker must not look like a broken edit.

A failed edit is skipped. The file on disk is not what the model tried to
write, so any finding would describe the previous content.

## `/lens`

Shows the findings for the file last written, as a toast. The model already saw
them, so this costs no context.

## Relation to pi-lens

The `pi-lens` plugin embeds LSP clients, tree-sitter grammars, ast-grep, and an
MCP server in about 26 MB of JavaScript. This is the edit-time diagnostics lane
only, shelling out to whatever the project already uses. Not ported: LSP
navigation, structural rules, the read guard, the dependency map, and the
impact cascade.

## Extending it

Add a case to `checkersFor` in `checker.go`. A checker needs a binary name,
its arguments, and optionally a config file that must exist in the project
root. `$FILE` is replaced with the written file and `$PKG` with its directory
as a package path.
