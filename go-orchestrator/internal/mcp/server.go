package mcp

import (
	"os"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/applemint-ltd/AdobePremiereProMCP/go-orchestrator/internal/audit"
)

const defaultMCPPageSize = 100

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
		server.WithToolHandlerMiddleware(requiredToolArgumentsMiddleware),
		server.WithLogging(),
		// Registered after WithRecovery so panics recovered there still flow
		// back through the audit middleware as errors and get recorded.
		server.WithToolHandlerMiddleware(newAuditMiddleware(aud, snap, logger)),
		server.WithPaginationLimit(mcpPageSize(logger)),
		server.WithInstructions("PremierPro MCP orchestrator — controls Adobe Premiere Pro 2026 through a curated, "+
			"verified-working tool set (project/media, timeline editing, captions, baked-PNG text, frame capture, real export). "+
			"Every tool call is audit-logged; premiere_get_session_digest and premiere_what_changed explain what happened. "+
			"On-screen text works ONLY via premiere_add_text_layer; captions ONLY via SRT import. "+
			"Set MCP_TOOL_PROFILE (core, standard, dialogue, captions, social, transitions, effects, proxies, delivery) "+
			"to load a workflow-sized catalog; tools known broken on 2026 are never exposed by any profile. "+
			"Read config://premiere-instructions for the golden storyboard->cut->export workflow."),
	)

	registerTools(s, orchestrator, logger)
	registerAuditTools(s, aud, snapshots, logger)

	// Order matters: drop the never-safe tools first so no profile — not even
	// "all" — can widen the surface back onto them. Profiles then shape what
	// remains, defaulting to "standard" (the verified everyday set, sized to
	// stay under the 128-function limit most model APIs enforce). The full
	// curated core is one env var away: MCP_TOOL_PROFILE=curated.
	stripUnsafeTools(s, logger)
	applyToolProfiles(s, logger)
	registerResources(s)
	registerPrompts(s)

	logger.Info("MCP server initialized",
		zap.String("name", "premierpro-mcp"),
		zap.String("version", version),
	)

	return s
}

func mcpPageSize(logger *zap.Logger) int {
	raw := strings.TrimSpace(os.Getenv("MCP_PAGE_SIZE"))
	if raw == "" {
		return defaultMCPPageSize
	}

	pageSize, err := strconv.Atoi(raw)
	if err != nil || pageSize <= 0 {
		logger.Warn("ignoring invalid MCP_PAGE_SIZE",
			zap.String("value", raw),
			zap.Int("default", defaultMCPPageSize),
		)
		return defaultMCPPageSize
	}
	return pageSize
}
