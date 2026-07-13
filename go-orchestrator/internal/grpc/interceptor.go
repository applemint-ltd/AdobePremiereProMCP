package grpc

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	premierepb "github.com/anthropics/premierpro-mcp/gen/go/premierpro/premiere/v1"
	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/audit"
)

// correlationUnaryInterceptor forwards the audit correlation ID to the
// ts-bridge as gRPC metadata and records each bridge hop (with the real
// ExtendScript function name for EvalCommand) on the request's audit span.
// Installed only on the Premiere-bridge dial so media/intel calls stay
// untouched.
func correlationUnaryInterceptor(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	span := audit.SpanFrom(ctx)
	if span != nil {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-correlation-id", span.CID)
	}

	start := time.Now()
	err := invoker(ctx, method, req, reply, cc, opts...)

	if span != nil {
		name := method
		if i := strings.LastIndex(method, "/"); i >= 0 {
			name = method[i+1:]
		}
		if r, ok := req.(*premierepb.EvalCommandRequest); ok {
			name = r.GetFunctionName()
		}
		span.AddESCall(name, time.Since(start).Milliseconds())
	}
	return err
}
