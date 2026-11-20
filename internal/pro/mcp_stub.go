package pro

// MCPBridge is retained for Pro builds that may wrap or extend the core MCP
// stdio server (internal/transport/mcp). The open MIT core already ships a
// functional MCP server via `mdcix mcp` — this interface is optional.
type MCPBridge interface {
	// Start would run an alternate MCP transport (e.g. hosted SSE) supplied by Pro.
	Start() error
}

// NoMCP means no Pro MCP overlay (core stdio MCP still available via mdcix mcp).
type NoMCP struct{}

// Start returns an error explaining that the core CLI MCP command should be used.
func (NoMCP) Start() error {
	return ErrMCPNotImplemented
}

// ErrMCPNotImplemented is returned when a Pro MCP overlay is requested but absent.
var ErrMCPNotImplemented = errMCP("pro MCP overlay not installed; use `mdcix mcp` for the core stdio server")

type errMCP string

func (e errMCP) Error() string { return string(e) }
