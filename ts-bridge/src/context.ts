import { AsyncLocalStorage } from "node:async_hooks";

/**
 * Per-request context threaded from the gRPC layer down to the CEP
 * WebSocket bridge. Carries the correlation ID the Go orchestrator's audit
 * middleware assigned to the originating MCP tool call, so one ID can be
 * grepped across orchestrator, bridge, and panel logs.
 */
export interface RequestContext {
  correlationId?: string;
}

export const requestContext = new AsyncLocalStorage<RequestContext>();
