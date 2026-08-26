package tools

import (
	"github.com/rapatel0/alpha/internal/tools/agenttool"
	"github.com/rapatel0/alpha/internal/tools/bashtool"
	"github.com/rapatel0/alpha/internal/tools/doctool"
	"github.com/rapatel0/alpha/internal/tools/findtool"
	"github.com/rapatel0/alpha/internal/tools/greptool"
	"github.com/rapatel0/alpha/internal/tools/lstool"
	"github.com/rapatel0/alpha/internal/tools/mcptool"
	"github.com/rapatel0/alpha/internal/tools/readimagetool"
	"github.com/rapatel0/alpha/internal/tools/readtool"
	"github.com/rapatel0/alpha/internal/tools/skilltool"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
	"github.com/rapatel0/alpha/internal/tools/webfetchtool"
	"github.com/rapatel0/alpha/internal/tools/websearchtool"
	"github.com/rapatel0/alpha/internal/tools/writetool"
)

type (
	// Result re-exports tooldef.Result.
	Result = tooldef.Result
	// Handler re-exports tooldef.Handler.
	Handler = tooldef.Handler
	// Tool re-exports tooldef.Tool.
	Tool = tooldef.Tool
	// Registry re-exports tooldef.Registry.
	Registry = tooldef.Registry
)

// Definitions and the registry helpers are re-exported from tooldef.
var (
	Definitions    = tooldef.Definitions
	NewRegistry    = tooldef.NewRegistry
	WithToolCallID = tooldef.WithToolCallID
	ToolCallID     = tooldef.ToolCallID
	WithCwd        = tooldef.WithCwd
	WithModel      = tooldef.WithModel
)

type (
	// ShellExecResult re-exports bashtool.ShellExecResult.
	ShellExecResult = bashtool.ShellExecResult
	// ShellExecOptions re-exports bashtool.ShellExecOptions.
	ShellExecOptions = bashtool.ShellExecOptions
	// BashOutputTail re-exports bashtool.BashOutputTail.
	BashOutputTail = bashtool.BashOutputTail
)

// Bash output limits are re-exported from bashtool.
const (
	BashMaxOutputLines = bashtool.BashMaxOutputLines
	BashMaxOutputBytes = bashtool.BashMaxOutputBytes
)

// ExecShell and NewBashOutputTail are re-exported from bashtool.
var (
	ExecShell         = bashtool.ExecShell
	NewBashOutputTail = bashtool.NewBashOutputTail
)

type (
	// AgentDeps re-exports agenttool.AgentDeps.
	AgentDeps = agenttool.AgentDeps
	// AgentResult re-exports agenttool.AgentResult.
	AgentResult = agenttool.AgentResult
)

// AgentTools, ParseAgentResult, and MCPTools are re-exported tool helpers.
var (
	AgentTools       = agenttool.AgentTools
	ParseAgentResult = agenttool.ParseAgentResult
	MCPTools         = mcptool.Tools
)

// DefaultTools returns the built-in agent tool set.
func DefaultTools() []Tool {
	return []Tool{
		bashtool.BashTool(),
		readtool.ReadTool(),
		readimagetool.ReadImageTool(),
		doctool.DocTool(),
		writetool.WriteTool(),
		greptool.GrepTool(),
		lstool.LsTool(),
		writetool.EditTool(),
		findtool.FindTool(),
		skilltool.SkillTool(),
		webfetchtool.WebFetchTool(),
		websearchtool.WebSearchTool(),
	}
}

// ReadonlyTools returns exploration tools without write/edit.
// Bash remains available but should be paired with ModeReadonly so only
// allowlisted commands run (no file mutations via the shell).
func ReadonlyTools() []Tool {
	return []Tool{
		bashtool.BashTool(),
		readtool.ReadTool(),
		readimagetool.ReadImageTool(),
		doctool.DocTool(),
		greptool.GrepTool(),
		lstool.LsTool(),
		findtool.FindTool(),
		skilltool.SkillTool(),
		webfetchtool.WebFetchTool(),
		websearchtool.WebSearchTool(),
	}
}
