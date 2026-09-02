package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func gatewayForwardContext(routeCtx context.Context, switchCount int, bridgeMetadata bool) context.Context {
	if routeCtx == nil {
		routeCtx = context.Background()
	}
	if switchCount > 0 {
		return service.WithAccountSwitchCount(routeCtx, switchCount, bridgeMetadata)
	}
	return routeCtx
}
