//go:build unit

package service

import (
	"io"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"ikik-api/internal/config"
)

func TestAccountTestService_CustomAPIKeyUsesChatCompletionsPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newTestContext()

	account := &Account{
		ID:          31,
		Name:        "custom-upstream",
		Platform:    PlatformCustom,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://custom-upstream.example.com",
			"api_key":  "custom-api-key",
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &queuedHTTPUpstream{
		responses: []*http.Response{
			newJSONResponse(http.StatusOK, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"),
		},
	}
	svc := &AccountTestService{
		accountRepo:         repo,
		httpUpstream:        upstream,
		cfg:                 &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}

	err := svc.TestAccountConnection(ctx, account.ID, "gpt-test", "", AccountTestModeDefault)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)

	req := upstream.requests[0]
	require.Equal(t, "https://custom-upstream.example.com/v1/chat/completions", req.URL.String())
	require.Equal(t, "Bearer custom-api-key", req.Header.Get("Authorization"))

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"stream":true}`, string(body))
}
