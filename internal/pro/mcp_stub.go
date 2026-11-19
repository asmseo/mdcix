package pro

// MCPBridge is a placeholder for a future Model Context Protocol stdio server.
//
// The open MIT core intentionally does not ship a full MCP implementation in the
// MVP — the universal integration path is the HTTP proxy (HTTP_PROXY/HTTPS_PROXY).
// A Pro or phase-2 build may provide a real Bridge without changing the core API.
type MCPBridge interface {
	// Start would run an MCP stdio (or SSE) server that exposes fetch/convert tools.
	Start() error
}

// NoMCP is the default: MCP disabled.
type NoMCP struct{}

// Start returns an error explaining that proxy/fetch should be used instead.
func (NoMCP) Start() error {
	return ErrMCPNotImplemented
}

// ErrMCPNotImplemented is returned by the MVP stub.
var ErrMCPNotImplemented = errMCP("mcp not implemented in core MVP; use HTTP_PROXY or mdcix fetch")

type errMCP string

func (e errMCP) Error() string { return string(e) }
