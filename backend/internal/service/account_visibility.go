package service

import (
	"context"

	"ikik-api/internal/pkg/ctxkey"
)

func AuthenticatedUserIDFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	userID, _ := ctx.Value(ctxkey.AuthenticatedUserID).(int64)
	return userID
}

func IsAccountVisibleToRequestUser(ctx context.Context, account *Account) bool {
	if !IsAccountAllowedForRequestGroup(ctx, account) {
		return false
	}
	return account.IsVisibleToConsumer(AuthenticatedUserIDFromContext(ctx))
}

func IsAccountAllowedForRequestGroup(ctx context.Context, account *Account) bool {
	if account == nil {
		return false
	}
	group := GroupFromContext(ctx)
	if group != nil &&
		(group.IsUserPrivateScope() || group.IsUserCarpoolScope()) &&
		NormalizeAccountShareMode(account.ShareMode) == AccountShareModePublic {
		return false
	}
	return true
}

func GroupFromContext(ctx context.Context) *Group {
	if ctx == nil {
		return nil
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	return group
}

func FilterAccountsVisibleToRequestUser(ctx context.Context, accounts []Account) []Account {
	if len(accounts) == 0 {
		return accounts
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if IsAccountVisibleToRequestUser(ctx, &account) {
			filtered = append(filtered, account)
		}
	}
	return filtered
}
