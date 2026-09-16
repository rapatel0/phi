package lens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
	"github.com/rapatel0/alpha/internal/util"
)

const (
	// navMaxFiles bounds the walk so a huge checkout still answers in one
	// turn. Deeper search belongs in a sub-agent with its own budget.
	navMaxFiles = 2000

	// navMaxHits caps reported matches. Past this the model is skimming, not
	// reading.
	navMaxHits = 40

	// hoverContext is the number of lines around a definition in a hover body.
	hoverContext = 3
)

// DiagnosticsTool returns lsp_diagnostics: checker findings for named files.
//
// The post-tool hook already reports findings for a file the model just wrote.
// This covers the other case: a file that has not been touched this session,
// where nothing would otherwise fire.
func DiagnosticsTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "lsp_diagnostics",
			Description: `Report checker findings for files without editing them.

Runs the checkers this project already uses (go vet, ruff, shellcheck, cargo check, tsc). Read-only: use it before a change to see the current state.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"paths": llm.Object{
						"type":        "array",
						"items":       llm.Object{"type": "string"},
						"description": "Files to check, relative to the working directory.",
					},
				},
				Required: []string{"paths"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Paths []string `json:"paths"`
			}
			_ = json.Unmarshal(input, &in)
			return strings.Join(in.Paths, " ")
		},
		Run: runDiagnostics,
	}
}

func runDiagnostics(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("lsp_diagnostics args: %w", err)
	}
	root, err := tooldef.Cwd(ctx)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("lsp_diagnostics: %w", err)
	}

	var all []Problem
	for _, path := range in.Paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		all = append(all, Check(ctx, root, path, checkTimeout)...)
	}
	if len(all) == 0 {
		return tooldef.Result{Content: "no problems", Detail: "clean", Output: "no problems"}, nil
	}
	body := note(all)
	return tooldef.Result{Content: body, Output: body, Detail: count(len(all))}, nil
}

// NavigationTool returns lsp_navigation: where a symbol is defined, referenced,
// or shown with its surrounding lines.
func NavigationTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "lsp_navigation",
			Description: `Locate a symbol: its definition, its references, or a hover-style block.

Definition is the default operation. Text matching, not an indexed language server: expect exact names to work and heavily overloaded names to return several hits.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"symbol": llm.Object{
						"type":        "string",
						"description": "Identifier to locate. Example: NewExecutor",
					},
					"path": llm.Object{
						"type":        "string",
						"description": "Optional file to search instead of the whole workspace.",
					},
					"operation": llm.Object{
						"type":        "string",
						"description": "definition (default), references, or hover.",
					},
				},
				Required: []string{"symbol"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Symbol string `json:"symbol"`
			}
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Symbol)
		},
		Run: runNavigation,
	}
}

func runNavigation(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in struct {
		Symbol    string `json:"symbol"`
		Path      string `json:"path"`
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("lsp_navigation args: %w", err)
	}
	symbol := strings.TrimSpace(in.Symbol)
	if symbol == "" {
		return tooldef.Result{}, errors.New("lsp_navigation: symbol is required")
	}
	root, err := tooldef.Cwd(ctx)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("lsp_navigation: %w", err)
	}

	hits := collectHits(root, strings.TrimSpace(in.Path), symbol)
	if len(hits) == 0 {
		return tooldef.Result{Content: "no matches", Detail: symbol, Output: "no matches"}, nil
	}

	op := strings.ToLower(strings.TrimSpace(in.Operation))
	if op == "" {
		op = "definition"
	}

	switch op {
	case "references":
		refs := make([]hit, 0, len(hits))
		for _, h := range hits {
			if !h.definition {
				refs = append(refs, h)
			}
		}
		if len(refs) == 0 {
			refs = hits
		}
		return hitsResult(symbol, "references", refs)
	case "hover":
		for _, h := range hits {
			if h.definition {
				body := hoverBlock(h)
				return tooldef.Result{Content: body, Output: body, Detail: symbol}, nil
			}
		}
		body := hoverBlock(hits[0])
		return tooldef.Result{Content: body, Output: body, Detail: symbol}, nil
	default:
		defs := make([]hit, 0, len(hits))
		for _, h := range hits {
			if h.definition {
				defs = append(defs, h)
			}
		}
		if len(defs) == 0 {
			defs = hits
		}
		return hitsResult(symbol, "definition", defs)
	}
}

// hit is one matching line. `definition` stays out of the JSON payload: the
// operation field already tells the model which kind of hit it asked for.
type hit struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Code       string `json:"code"`
	definition bool
}

type hitView struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Code string `json:"code"`
}

func hitsResult(symbol, op string, hits []hit) (tooldef.Result, error) {
	rows := make([]hitView, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, hitView{File: h.File, Line: h.Line, Col: h.Col, Code: h.Code})
	}
	body := util.MustJSONIndent(map[string]any{
		"symbol":    symbol,
		"operation": op,
		"results":   rows,
		"count":     len(rows),
	})
	return tooldef.Result{Content: body, Output: body, Detail: fmt.Sprintf("%d hits", len(rows))}, nil
}

func hoverBlock(h hit) string {
	return fmt.Sprintf("%s:%d:%d\n%s", h.File, h.Line, h.Col, h.Code)
}

// collectHits walks the workspace (or one file) and returns matching lines.
func collectHits(root, only, symbol string) []hit {
	var hits []hit
	add := func(path string, lineNo int, line string) {
		col := strings.Index(line, symbol)
		if col < 0 {
			col = 0
		}
		hits = append(hits, hit{
			File:       path,
			Line:       lineNo,
			Col:        col + 1,
			Code:       strings.TrimRight(line, "\r"),
			definition: isDefinition(line, symbol),
		})
	}

	if only != "" {
		full := only
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, only)
		}
		scanFile(full, only, symbol, add)
		return hits
	}

	scanned := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(hits) >= navMaxHits || scanned >= navMaxFiles {
			return fs.SkipAll
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if skipFile(d.Name()) {
			return nil
		}
		scanned++
		scanFile(path, relFor(root, path), symbol, add)
		return nil
	})
	return hits
}

func scanFile(full, shown, symbol string, add func(string, int, string)) {
	raw, err := readFile(full)
	if err != nil {
		return
	}
	for i, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, symbol) {
			add(shown, i+1, strings.TrimSpace(line))
		}
	}
}

// defKeywords are the words that open a declaration in the languages this
// project tends to hold: Go, Python, TypeScript, Rust, shell.
var defKeywords = []string{
	"func ", "type ", "const ", "var ", "struct ", "interface ", "package ",
	"def ", "class ", "enum ", "fn ", "let ", "val ", "pub ", "mod ",
	"export ", "public ", "private ", "protected ", "namespace ",
}

// isDefinition reports whether one line reads as the declaration of symbol.
func isDefinition(line, symbol string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	for _, kw := range defKeywords {
		if strings.HasPrefix(t, kw) && strings.Contains(t, symbol) {
			return true
		}
	}
	// Package-level declarations in Go, Python, and shell do not open with a
	// keyword, so match those assignment shapes too. A bare call, `Helper()`,
	// is deliberately not a shape here: it would mark every call site as a
	// declaration and leave the references operation with nothing to show.
	for _, shape := range []string{symbol + " :=", symbol + " ="} {
		if strings.HasPrefix(t, shape) {
			return true
		}
	}
	return false
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", "venv", ".venv", "__pycache__":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func skipFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".woff", ".woff2", ".ttf", ".zip", ".gz", ".pdf", ".so", ".dylib":
		return true
	}
	return false
}

// relFor shows a workspace-relative path to the model, matching the other
// tools. A path outside the workspace is returned unchanged.
func relFor(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// readFile keeps the scan in one place so tests can point it at a fixture.
func readFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}
