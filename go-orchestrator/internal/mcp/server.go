package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/audit"
)

// NewMCPServer creates and configures an MCP server that exposes all
// Premiere Pro editing tools to AI clients. The returned server is ready
// to be served over stdio or any other transport supported by mcp-go.
//
// The orchestrator parameter provides the concrete implementation that
// each tool handler delegates to for performing actual editing operations.
// aud and snapshots may be nil, which disables audit persistence and the
// snapshot-backed tools (the middleware still assigns correlation IDs).
// autoSnapshot controls whether a timeline snapshot is taken before every
// mutating tool call; the diff/digest tools work off stored snapshots either
// way.
func NewMCPServer(orchestrator Orchestrator, version string, logger *zap.Logger, aud *audit.Auditor, snapshots *audit.SnapshotStore, autoSnapshot bool) *server.MCPServer {
	if version == "" {
		version = "dev"
	}

	var snap Snapshotter
	if autoSnapshot && snapshots != nil {
		snap = snapshots
	}

	s := server.NewMCPServer(
		"premierpro-mcp",
		version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(false, true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
		server.WithLogging(),
		// Registered after WithRecovery so panics recovered there still flow
		// back through the audit middleware as errors and get recorded.
		server.WithToolHandlerMiddleware(newAuditMiddleware(aud, snap, logger)),
		server.WithInstructions("PremierPro MCP orchestrator — controls Adobe Premiere Pro through natural language. "+
			"Available tool categories: project inspection, media scanning, timeline editing, "+
			"script-to-edit pipeline, and export. "+
			"Read config://premiere-instructions for detailed usage guidance."),
	)

	registerTools(s, orchestrator, logger)
	registerAuditTools(s, aud, snapshots, logger)
	registerResources(s)
	registerPrompts(s)

	logger.Info("MCP server initialized",
		zap.String("name", "premierpro-mcp"),
		zap.String("version", version),
	)

	return s
}
