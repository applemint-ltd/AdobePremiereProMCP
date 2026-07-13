package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TransportType defines how the MCP server communicates with the host.
type TransportType string

const (
	TransportStdio TransportType = "stdio"
	TransportSSE   TransportType = "sse"
)

// Config holds all configuration for the Go orchestrator.
type Config struct {
	// Transport is the MCP transport type (stdio or sse).
	Transport TransportType

	// SSEPort is the port for the SSE HTTP server (only used when Transport == "sse").
	SSEPort int

	// LogLevel controls the zap log verbosity (debug, info, warn, error).
	LogLevel string

	// Service addresses for gRPC connections.
	RustEngineAddr       string
	PythonIntelAddr      string
	TypeScriptBridgeAddr string

	// Per-service timeouts.
	RustEngineTimeout       time.Duration
	PythonIntelTimeout      time.Duration
	TypeScriptBridgeTimeout time.Duration

	// AuditDir is where per-tool-call audit JSONL files (and timeline
	// snapshots) are written. Empty disables auditing.
	AuditDir string

	// SessionTag identifies the logical session (e.g. a Slack thread key)
	// stamped on every audit record. Set by whatever launches the server.
	SessionTag string

	// ClaudeSession is the Claude Code session ID driving this server, when
	// known, so audit records can be joined to the client-side transcript.
	ClaudeSession string

	// AutoSnapshot controls whether a timeline snapshot is taken before each
	// mutating tool call.
	AutoSnapshot bool
}

// Defaults returns a Config populated with default values.
func Defaults() Config {
	return Config{
		Transport:               TransportStdio,
		SSEPort:                 8080,
		LogLevel:                "info",
		RustEngineAddr:          "localhost:50052",
		PythonIntelAddr:         "localhost:50053",
		TypeScriptBridgeAddr:    "localhost:50054",
		RustEngineTimeout:       30 * time.Second,
		PythonIntelTimeout:      60 * time.Second,
		TypeScriptBridgeTimeout: 30 * time.Second,
		AuditDir:                defaultAuditDir(),
		AutoSnapshot:            true,
	}
}

// defaultAuditDir keeps auditing on even when the launcher forgets to set
// PREMIERE_AUDIT_DIR — traceability is the point, so opt-out, not opt-in.
func defaultAuditDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "premierpro-mcp", "audit")
}

// LoadFromEnv returns a Config by starting from defaults and overriding with
// any environment variables that are set.
//
// Supported environment variables:
//
//	MCP_TRANSPORT           - "stdio" or "sse"
//	MCP_SSE_PORT            - port number for SSE transport
//	MCP_LOG_LEVEL           - "debug", "info", "warn", "error"
//	RUST_ENGINE_ADDR        - gRPC address for the Rust media engine
//	PYTHON_INTEL_ADDR       - gRPC address for the Python intelligence service
//	TS_BRIDGE_ADDR          - gRPC address for the TypeScript Premiere bridge
//	RUST_ENGINE_TIMEOUT     - timeout in seconds for Rust engine calls
//	PYTHON_INTEL_TIMEOUT    - timeout in seconds for Python intelligence calls
//	TS_BRIDGE_TIMEOUT       - timeout in seconds for TypeScript bridge calls
//	PREMIERE_AUDIT_DIR      - directory for tool-call audit JSONL + snapshots
//	                          ("off" disables auditing entirely)
//	PREMIERE_SESSION_TAG    - session tag (e.g. Slack thread key) for audit records
//	PREMIERE_CLAUDE_SESSION - Claude Code session ID for audit records
//	PREMIERE_AUTO_SNAPSHOT  - "off"/"0"/"false" disables pre-mutation timeline snapshots
func LoadFromEnv() (Config, error) {
	cfg := Defaults()

	if v := os.Getenv("MCP_TRANSPORT"); v != "" {
		switch TransportType(v) {
		case TransportStdio, TransportSSE:
			cfg.Transport = TransportType(v)
		default:
			return cfg, fmt.Errorf("invalid MCP_TRANSPORT %q: must be \"stdio\" or \"sse\"", v)
		}
	}

	if v := os.Getenv("MCP_SSE_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid MCP_SSE_PORT %q: %w", v, err)
		}
		if port < 1 || port > 65535 {
			return cfg, fmt.Errorf("MCP_SSE_PORT %d out of range (1-65535)", port)
		}
		cfg.SSEPort = port
	}

	if v := os.Getenv("MCP_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("RUST_ENGINE_ADDR"); v != "" {
		cfg.RustEngineAddr = v
	}
	if v := os.Getenv("PYTHON_INTEL_ADDR"); v != "" {
		cfg.PythonIntelAddr = v
	}
	if v := os.Getenv("TS_BRIDGE_ADDR"); v != "" {
		cfg.TypeScriptBridgeAddr = v
	}

	if v := os.Getenv("RUST_ENGINE_TIMEOUT"); v != "" {
		d, err := parseTimeoutSeconds(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid RUST_ENGINE_TIMEOUT %q: %w", v, err)
		}
		cfg.RustEngineTimeout = d
	}
	if v := os.Getenv("PYTHON_INTEL_TIMEOUT"); v != "" {
		d, err := parseTimeoutSeconds(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid PYTHON_INTEL_TIMEOUT %q: %w", v, err)
		}
		cfg.PythonIntelTimeout = d
	}
	if v := os.Getenv("TS_BRIDGE_TIMEOUT"); v != "" {
		d, err := parseTimeoutSeconds(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TS_BRIDGE_TIMEOUT %q: %w", v, err)
		}
		cfg.TypeScriptBridgeTimeout = d
	}

	if v := os.Getenv("PREMIERE_AUDIT_DIR"); v != "" {
		if v == "off" {
			cfg.AuditDir = ""
		} else {
			cfg.AuditDir = v
		}
	}
	cfg.SessionTag = envOrEmpty("PREMIERE_SESSION_TAG")
	cfg.ClaudeSession = envOrEmpty("PREMIERE_CLAUDE_SESSION")
	switch os.Getenv("PREMIERE_AUTO_SNAPSHOT") {
	case "off", "0", "false":
		cfg.AutoSnapshot = false
	}

	return cfg, nil
}

// envOrEmpty reads an env var, treating an unexpanded "${...}" placeholder
// (which .mcp.json interpolation passes through when the variable is unset
// in the launching environment) as absent.
func envOrEmpty(key string) string {
	v := os.Getenv(key)
	if strings.HasPrefix(v, "${") {
		return ""
	}
	return v
}

func parseTimeoutSeconds(s string) (time.Duration, error) {
	secs, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if secs <= 0 {
		return 0, fmt.Errorf("timeout must be positive, got %d", secs)
	}
	return time.Duration(secs) * time.Second, nil
}
