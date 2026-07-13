package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/audit"
	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/config"
	grpcclients "github.com/anthropics/premierpro-mcp/go-orchestrator/internal/grpc"
	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/health"
	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/mcp"
	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/orchestrator"
)

// Build-time variables set via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// ── Flags ──────────────────────────────────────────────────────────
	var (
		transport = flag.String("transport", "", `MCP transport: "stdio" (default) or "sse"`)
		port      = flag.Int("port", 0, "SSE HTTP port (only used with --transport=sse)")
		logLevel  = flag.String("log-level", "", `Log level: "debug", "info", "warn", "error"`)
	)
	flag.Parse()

	// ── Config ────────────────────────────────────────────────────────
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// CLI flags override env/defaults.
	if *transport != "" {
		cfg.Transport = config.TransportType(*transport)
	}
	if *port != 0 {
		cfg.SSEPort = *port
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}

	// ── Logger ────────────────────────────────────────────────────────
	logger, err := buildLogger(cfg.LogLevel, cfg.LogDir)
	if err != nil {
		return fmt.Errorf("building logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting premierpro-mcp orchestrator",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("built", date),
		zap.String("transport", string(cfg.Transport)),
	)

	// ── gRPC client connections ───────────────────────────────────────
	clients, err := grpcclients.NewClients(&grpcclients.ClientsConfig{
		MediaAddr:    cfg.RustEngineAddr,
		IntelAddr:    cfg.PythonIntelAddr,
		PremiereAddr: cfg.TypeScriptBridgeAddr,
		DialTimeout:  cfg.RustEngineTimeout,
		CallTimeout:  cfg.TypeScriptBridgeTimeout,
	}, logger)
	if err != nil {
		return fmt.Errorf("connecting gRPC clients: %w", err)
	}
	defer clients.Close()

	logger.Info("all gRPC clients connected",
		zap.String("media", cfg.RustEngineAddr),
		zap.String("intel", cfg.PythonIntelAddr),
		zap.String("premiere", cfg.TypeScriptBridgeAddr),
	)

	// ── Orchestrator Engine ──────────────────────────────────────────
	engine := orchestrator.New(
		&grpcclients.MediaAdapter{C: clients.Media},
		&grpcclients.IntelAdapter{C: clients.Intel},
		&grpcclients.PremiereAdapter{C: clients.Premiere},
		logger,
	)

	// ── Audit trail ──────────────────────────────────────────────────
	auditor := audit.NewAuditor(cfg.AuditDir, cfg.SessionTag, cfg.ClaudeSession)
	captureTimeline := func(ctx context.Context) (string, error) {
		res, err := engine.SnapshotTimeline(ctx, -1)
		if err != nil {
			return "", err
		}
		return res.Message, nil
	}
	snapshots := audit.NewSnapshotStore(cfg.AuditDir, cfg.SessionTag, captureTimeline)
	if auditor != nil {
		logger.Info("audit trail enabled",
			zap.String("dir", cfg.AuditDir),
			zap.String("session", cfg.SessionTag),
			zap.Bool("auto_snapshot", cfg.AutoSnapshot),
		)
	}

	// ── MCP Server ────────────────────────────────────────────────────
	mcpSrv := mcp.NewMCPServer(engine, version, logger, auditor, snapshots, cfg.AutoSnapshot)

	// ── Serve ─────────────────────────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Health endpoint ───────────────────────────────────────────────
	if cfg.HealthPort > 0 {
		checker := health.NewChecker(logger)
		checker.RegisterProbe("premiere_bridge", func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, err := clients.Premiere.Ping(ctx)
			return err
		})
		checker.RegisterProbe("media_engine", health.MediaProbe(clients.Media))
		checker.RegisterProbe("intelligence", health.IntelligenceProbe(clients.Intel))
		checker.Start(ctx, 30*time.Second)

		healthSrv := &http.Server{
			Addr:    fmt.Sprintf("localhost:%d", cfg.HealthPort),
			Handler: health.NewHTTPHandler(checker),
		}
		go func() {
			// Several MCP server processes can coexist (one per headless
			// claude turn); only the first gets the port. That is fine —
			// any live process can serve health for the shared backends.
			if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Info("health endpoint not started (likely another instance owns the port)",
					zap.Int("port", cfg.HealthPort), zap.Error(err))
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = healthSrv.Shutdown(shutdownCtx)
		}()
	}

	switch cfg.Transport {
	case config.TransportSSE:
		return serveSSE(ctx, mcpSrv, cfg, logger)
	default:
		return serveStdio(ctx, mcpSrv, logger)
	}
}

// serveStdio runs the MCP server over stdin/stdout.
func serveStdio(_ context.Context, mcpSrv *mcpserver.MCPServer, logger *zap.Logger) error {
	logger.Info("serving MCP over stdio")
	return mcpserver.ServeStdio(mcpSrv)
}

// serveSSE runs the MCP server as an HTTP SSE endpoint.
func serveSSE(ctx context.Context, mcpSrv *mcpserver.MCPServer, cfg config.Config, logger *zap.Logger) error {
	addr := fmt.Sprintf(":%d", cfg.SSEPort)
	logger.Info("serving MCP over SSE", zap.String("addr", addr))

	sseSrv := mcpserver.NewSSEServer(
		mcpSrv,
		mcpserver.WithBaseURL(fmt.Sprintf("http://localhost:%d", cfg.SSEPort)),
	)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return sseSrv.Start(addr)
	})

	g.Go(func() error {
		<-ctx.Done()
		logger.Info("shutting down SSE server")
		return sseSrv.Shutdown(context.Background())
	})

	return g.Wait()
}

// ── Helpers ───────────────────────────────────────────────────────────

// buildLogger creates a zap.Logger that always writes to stderr at the
// requested level and, when logDir is set, additionally to a rotated file at
// Info. The file core is independent of the stderr level on purpose: the
// stdio launcher runs with --log-level error, and per-tool-call visibility
// must survive that.
func buildLogger(level, logDir string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zap.DebugLevel
	case "warn":
		zapLevel = zap.WarnLevel
	case "error":
		zapLevel = zap.ErrorLevel
	default:
		zapLevel = zap.InfoLevel
	}

	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	consoleCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stderr),
		zap.NewAtomicLevelAt(zapLevel),
	)

	core := consoleCore
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			fileLevel := zapLevel
			if fileLevel > zap.InfoLevel {
				fileLevel = zap.InfoLevel
			}
			fileCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderCfg),
				zapcore.AddSync(&lumberjack.Logger{
					Filename:   filepath.Join(logDir, "go-orchestrator.log"),
					MaxSize:    50, // MB
					MaxBackups: 5,
				}),
				zap.NewAtomicLevelAt(fileLevel),
			)
			core = zapcore.NewTee(consoleCore, fileCore)
		}
	}

	return zap.New(core, zap.AddCaller()), nil
}
