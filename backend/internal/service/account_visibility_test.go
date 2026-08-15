package service

import (
	"context"
	"testing"

	"ikik-api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestIsAccountVisibleToRequestUserExcludesPublicShareModeFromPrivateGroup(t *testing.T) {
	ownerID := int64(42)
	account := &Account{
		ID:          10,
		OwnerUserID: &ownerID,
		ShareMode:   AccountShareModePublic,
		ShareStatus: AccountShareStatusPending,
	}
	ctx := context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, ownerID)
	ctx = context.WithValue(ctx, ctxkey.Group, &Group{
		ID:          99,
		Scope:       GroupScopeUserPrivate,
		OwnerUserID: &ownerID,
	})

	require.False(t, IsAccountVisibleToRequestUser(ctx, account))

	account.ShareMode = AccountShareModePrivate
	require.True(t, IsAccountVisibleToRequestUser(ctx, account))
}

func TestIsAccountVisibleToRequestUserKeepsApprovedPublicShareVisibleInPublicGroup(t *testing.T) {
	ownerID := int64(42)
	consumerID := int64(100)
	account := &Account{
		ID:          10,
		OwnerUserID: &ownerID,
		ShareMode:   AccountShareModePublic,
		ShareStatus: AccountShareStatusApproved,
	}
	ctx := context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, consumerID)
	ctx = context.WithValue(ctx, ctxkey.Group, &Group{
		ID:    6,
		Scope: GroupScopePublic,
	})

	require.True(t, IsAccountVisibleToRequestUser(ctx, account))
}
