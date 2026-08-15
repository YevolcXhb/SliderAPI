package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"ikik-api/internal/pkg/claudeweb"
	"ikik-api/internal/pkg/ctxkey"

	"github.com/stretchr/testify/require"
)

func TestBuildClaudeWebPromptModeLatestTurnOnly(t *testing.T) {
	body := []byte(`{
		"system":"system rule",
		"messages":[
			{"role":"user","content":"first question"},
			{"role":"assistant","content":"first answer"},
			{"role":"user","content":"latest question"}
		]
	}`)

	full, fullTokens, err := buildClaudeWebPromptMode(body, false)
	require.NoError(t, err)
	require.Contains(t, full, "system rule")
	require.Contains(t, full, "first question")
	require.Contains(t, full, "latest question")
	require.Positive(t, fullTokens)

	latest, latestTokens, err := buildClaudeWebPromptMode(body, true)
	require.NoError(t, err)
	require.NotContains(t, latest, "system rule")
	require.NotContains(t, latest, "first question")
	require.Contains(t, latest, "latest question")
	require.Less(t, latestTokens, fullTokens)
}

func TestClaudeWebConversationKeyIsIsolated(t *testing.T) {
	groupID := int64(9)
	parsed := &ParsedRequest{ExplicitSessionID: "conversation-1", GroupID: &groupID}
	account := &Account{ID: 17}
	first := claudeWebConversationKey(context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, int64(5)), account, parsed)
	second := claudeWebConversationKey(context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, int64(6)), account, parsed)
	require.NotEmpty(t, first)
	require.NotEqual(t, first, second)
	require.NotContains(t, first, "conversation-1")
}

func TestClaudeWebConversationStorePersistsAndInvalidates(t *testing.T) {
	store := newClaudeWebConversationStore(0, 2)
	state := store.acquire("one")
	state.conversationID = "conversation-upstream"
	state.lastAssistantUUID = "assistant-1"
	store.release(state)

	again := store.acquire("one")
	require.Equal(t, "conversation-upstream", again.conversationID)
	require.Equal(t, "assistant-1", again.lastAssistantUUID)
	store.invalidate("one", again)
	store.release(again)

	replacement := store.acquire("one")
	require.Empty(t, replacement.conversationID)
	store.release(replacement)
}

func TestClaudeWebUnsupportedModelReturnsClientError(t *testing.T) {
	err := (&GatewayService{}).claudeWebForwardError(
		context.Background(),
		&Account{ID: 17},
		&claudeweb.UnsupportedModelError{Model: "claude-unknown"},
	)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 400, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "invalid_request_error")
	require.Contains(t, string(failoverErr.ResponseBody), "claude-unknown")
}

func TestClaudeWebConversationStoreConcurrentAccess(t *testing.T) {
	store := newClaudeWebConversationStore(0, 128)
	var wait sync.WaitGroup
	for worker := 0; worker < 64; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 50; iteration++ {
				key := fmt.Sprintf("session-%d", worker%8)
				state := store.acquire(key)
				state.conversationID = fmt.Sprintf("conversation-%d-%d", worker, iteration)
				state.lastAssistantUUID = fmt.Sprintf("assistant-%d-%d", worker, iteration)
				store.release(state)
			}
		}()
	}
	wait.Wait()

	store.mu.Lock()
	require.Len(t, store.entries, 8)
	store.mu.Unlock()
}
