package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterUserRoutes 注册用户相关路由（需要认证）
func RegisterUserRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	public := v1.Group("/public")
	{
		usage := public.Group("/usage")
		{
			usage.GET("/today", h.Usage.PublicTodayStats)
		}
	}

	shopPublic := v1.Group("/shop")
	{
		shopPublic.GET("/categories", h.Shop.ListCategories)
		shopPublic.GET("/products", h.Shop.ListProducts)
		shopPublic.GET("/products/:id", h.Shop.GetProduct)
	}

	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	// 面板全局按用户限流：防止单个账号高频刷接口打爆数据库
	authenticated.Use(panelRateLimiter.Global())
	// 用户管理面变更类操作入审计（含 TOTP 启用/禁用、step-up 验证、密码修改等安全事件）
	authenticated.Use(gin.HandlerFunc(auditLog))

	shop := authenticated.Group("/shop")
	{
		shop.GET("/draw-progress", h.Shop.ListDrawProgress)
		shop.POST("/orders", h.Shop.CreateOrder)
		shop.GET("/orders/:id", h.Shop.GetOrder)
		shop.GET("/orders/:id/files/download.zip", h.Shop.DownloadOrderFilesZip)
		shop.GET("/orders/:id/files/:card_id/download", h.Shop.DownloadOrderFile)
	}

	{

		// 用户接口
		user := authenticated.Group("/user")
		{
			user.GET("/profile", h.User.GetProfile)
			user.PUT("/password", h.User.ChangePassword)
			user.PUT("", h.User.UpdateProfile)
			user.GET("/receipt-code", h.ReceiptCode.Get)
			user.POST("/receipt-code", h.ReceiptCode.Upload)
			user.DELETE("/receipt-code", h.ReceiptCode.Delete)
			user.GET("/withdrawals", h.Withdrawal.ListMine)
			user.POST("/withdrawals", h.Withdrawal.Submit)
			user.POST("/withdrawals/:id/cancel", h.Withdrawal.Cancel)
			user.GET("/aff", h.User.GetAffiliate)
			user.POST("/aff/transfer", h.User.TransferAffiliateQuota)
			user.POST("/account-bindings/email/send-code", h.User.SendEmailBindingCode)
			user.POST("/account-bindings/email", h.User.BindEmailIdentity)
			user.DELETE("/account-bindings/:provider", h.User.UnbindIdentity)
			user.POST("/auth-identities/bind/start", h.User.StartIdentityBinding)
			user.GET("/api-keys/:id/usage/daily", panelRateLimiter.Heavy(), h.Usage.GetMyAPIKeyDailyUsage)
			user.GET("/platform-quotas", h.User.GetMyPlatformQuotas)

			// 通知邮箱管理
			notifyEmail := user.Group("/notify-email")
			{
				notifyEmail.POST("/send-code", h.User.SendNotifyEmailCode)
				notifyEmail.POST("/verify", h.User.VerifyNotifyEmail)
				notifyEmail.PUT("/toggle", h.User.ToggleNotifyEmail)
				notifyEmail.DELETE("", h.User.RemoveNotifyEmail)
			}

			// TOTP 双因素认证
			totp := user.Group("/totp")
			{
				totp.GET("/status", h.Totp.GetStatus)
				totp.GET("/verification-method", h.Totp.GetVerificationMethod)
				totp.POST("/send-code", h.Totp.SendVerifyCode)
				totp.POST("/setup", h.Totp.InitiateSetup)
				totp.POST("/enable", h.Totp.Enable)
				totp.POST("/disable", h.Totp.Disable)
				// 敏感操作二次验证：授予当前会话一段时间的 step-up 权限
				totp.POST("/step-up", h.Totp.StepUp)
			}

			passkeys := user.Group("/passkeys")
			{
				passkeys.GET("", h.Passkey.List)
				passkeys.POST("/register/begin", h.Passkey.BeginRegistration)
				passkeys.POST("/register/finish", h.Passkey.FinishRegistration)
				passkeys.PATCH("/:id", h.Passkey.Rename)
				passkeys.DELETE("/:id", h.Passkey.Delete)
			}
		}

		// API Key管理
		keys := authenticated.Group("/keys")
		{
			keys.GET("", h.APIKey.List)
			keys.GET("/:id", h.APIKey.GetByID)
			keys.POST("", h.APIKey.Create)
			keys.PUT("/:id", h.APIKey.Update)
			keys.DELETE("/:id", h.APIKey.Delete)
		}

		accounts := authenticated.Group("/accounts")
		{
			accounts.GET("", h.UserAccount.List)
			accounts.GET("/carpools", h.UserAccount.ListCarpools)
			accounts.GET("/carpools/hall", h.UserAccount.ListCarpoolHall)
			accounts.GET("/carpools/invite/:invite_code", h.UserAccount.GetCarpoolDetailByInviteCode)
			accounts.POST("/carpools/invite/:invite_code/apply", h.UserAccount.ApplyCarpoolByInviteCode)
			accounts.GET("/carpools/:pool_id", h.UserAccount.GetCarpoolDetail)
			accounts.POST("/carpools", h.UserAccount.CreateCarpool)
			accounts.DELETE("/carpools/:pool_id", h.UserAccount.DeleteCarpool)
			accounts.PUT("/carpools/:pool_id/accounts", h.UserAccount.BindCarpoolAccounts)
			accounts.POST("/carpools/:pool_id/accounts/:account_id/reset-local-limit", h.UserAccount.ResetCarpoolAccountLocalLimit)
			accounts.POST("/carpools/:pool_id/apply", h.UserAccount.ApplyCarpool)
			accounts.POST("/carpools/:pool_id/requests/:request_id/approve", h.UserAccount.ApproveCarpoolJoinRequest)
			accounts.POST("/carpools/:pool_id/requests/:request_id/reject", h.UserAccount.RejectCarpoolJoinRequest)
			accounts.POST("/carpools/:pool_id/requests/:request_id/confirm-paid", h.UserAccount.ConfirmCarpoolJoinPaid)
			accounts.POST("/carpools/:pool_id/members/:member_id/remove", h.UserAccount.RemoveCarpoolMember)
			accounts.PUT("/carpools/:pool_id/members/allocation", h.UserAccount.UpdateCarpoolMemberAllocations)
			accounts.GET("/quota-dashboard", h.UserAccount.GetQuotaPoolDashboard)
			accounts.GET("/:id/usage", h.UserAccount.GetUsage)
			accounts.GET("/:id/stats", h.UserAccount.GetStats)
			accounts.GET("/:id/today-stats", h.UserAccount.GetTodayStats)
			accounts.GET("/:id", h.UserAccount.GetByID)
			accounts.POST("", h.UserAccount.Create)
			accounts.POST("/import", h.UserAccount.Import)
			accounts.POST("/import-credentials", h.UserAccount.ImportCredentials)
			accounts.POST("/bulk-update", h.UserAccount.BulkUpdate)
			accounts.POST("/bulk-delete", h.UserAccount.BulkDelete)
			accounts.POST("/batch-refresh/async", h.UserAccount.CreateBatchRefreshTask)
			accounts.POST("/batch-revalidate-public-share/async", h.UserAccount.CreateBatchRevalidatePublicShareTask)
			accounts.GET("/batch-tasks/:task_id", h.UserAccount.GetBatchTask)
			accounts.POST("/:id/test", h.UserAccount.Test)
			accounts.POST("/:id/refresh", h.UserAccount.Refresh)
			accounts.POST("/:id/set-privacy", h.UserAccount.SetPrivacy)
			accounts.POST("/:id/revalidate-public-share", h.UserAccount.RevalidatePublicShare)
			accounts.PUT("/:id", h.UserAccount.Update)
			accounts.DELETE("/:id", h.UserAccount.Delete)
		}

		accountProxies := authenticated.Group("/account-proxies")
		{
			accountProxies.GET("", h.UserAccount.ListProxies)
			accountProxies.POST("", h.UserAccount.CreateProxy)
			accountProxies.PUT("/:id", h.UserAccount.UpdateProxy)
			accountProxies.DELETE("/:id", h.UserAccount.DeleteProxy)
			accountProxies.POST("/:id/test", h.UserAccount.TestProxy)
			accountProxies.POST("/:id/quality-check", h.UserAccount.CheckProxyQuality)
		}

		// User-scoped OAuth endpoints for creating personal accounts.
		accountOAuth := authenticated.Group("/account-oauth")
		{
			accountOAuth.POST("/anthropic/auth-url", h.UserAccount.GenerateAnthropicOAuthURL)
			accountOAuth.POST("/anthropic/exchange-code", h.UserAccount.ExchangeAnthropicOAuthCode)
			accountOAuth.POST("/anthropic/setup-token/auth-url", h.UserAccount.GenerateAnthropicSetupTokenURL)
			accountOAuth.POST("/anthropic/setup-token/exchange-code", h.UserAccount.ExchangeAnthropicSetupTokenCode)
			accountOAuth.POST("/anthropic/cookie-auth", h.UserAccount.AnthropicCookieAuth)
			accountOAuth.POST("/anthropic/setup-token-cookie-auth", h.UserAccount.AnthropicSetupTokenCookieAuth)
			accountOAuth.POST("/openai/auth-url", h.UserAccount.GenerateOpenAIOAuthURL)
			accountOAuth.POST("/openai/exchange-code", h.UserAccount.ExchangeOpenAIOAuthCode)
			accountOAuth.POST("/openai/refresh-token", h.UserAccount.RefreshOpenAIToken)
			accountOAuth.GET("/gemini/capabilities", h.UserAccount.GetGeminiOAuthCapabilities)
			accountOAuth.POST("/gemini/auth-url", h.UserAccount.GenerateGeminiOAuthURL)
			accountOAuth.POST("/gemini/exchange-code", h.UserAccount.ExchangeGeminiOAuthCode)
			accountOAuth.POST("/antigravity/auth-url", h.UserAccount.GenerateAntigravityOAuthURL)
			accountOAuth.POST("/antigravity/exchange-code", h.UserAccount.ExchangeAntigravityOAuthCode)
			accountOAuth.POST("/antigravity/refresh-token", h.UserAccount.RefreshAntigravityToken)
			accountOAuth.POST("/grok/auth-url", h.UserAccount.GenerateGrokOAuthURL)
			accountOAuth.POST("/grok/exchange-code", h.UserAccount.ExchangeGrokOAuthCode)
			accountOAuth.POST("/grok/refresh-token", h.UserAccount.RefreshGrokToken)
			accountOAuth.POST("/kiro/auth-url", h.UserAccount.GenerateKiroOAuthURL)
			accountOAuth.POST("/kiro/idc-auth-url", h.UserAccount.GenerateKiroIDCAuthURL)
			accountOAuth.POST("/kiro/exchange-code", h.UserAccount.ExchangeKiroOAuthCode)
			accountOAuth.POST("/kiro/refresh-token", h.UserAccount.RefreshKiroToken)
			accountOAuth.POST("/kiro/import-token", h.UserAccount.ImportKiroToken)
		}

		playground := authenticated.Group("/playground")
		{
			playground.POST("/chat/completions", h.Playground.ChatCompletions)
		}

		// 用户可用分组（非管理员接口）
		groups := authenticated.Group("/groups")
		{
			groups.GET("/available", h.APIKey.GetAvailableGroups)
			groups.GET("/rates", h.APIKey.GetUserGroupRates)
		}

		// 用户可用渠道（非管理员接口）
		channels := authenticated.Group("/channels")
		{
			channels.GET("/available", h.AvailableChannel.List)
		}

		// 使用记录（聚合统计属重查询，叠加更严格的按用户限流）
		usage := authenticated.Group("/usage")
		usage.Use(panelRateLimiter.Heavy())
		{
			usage.GET("", h.Usage.List)
			usage.GET("/errors", h.Usage.ListErrors)
			usage.GET("/errors/:id", h.Usage.GetErrorDetail)
			usage.GET("/:id", h.Usage.GetByID)
			usage.GET("/stats", h.Usage.Stats)
			// User dashboard endpoints
			usage.GET("/dashboard/stats", h.Usage.DashboardStats)
			usage.GET("/dashboard/trend", h.Usage.DashboardTrend)
			usage.GET("/dashboard/models", h.Usage.DashboardModels)
			usage.GET("/dashboard/snapshot-v2", h.Usage.DashboardSnapshotV2)
			usage.GET("/dashboard/account-sharing", h.Usage.DashboardAccountSharing)
			usage.POST("/dashboard/api-keys-usage", h.Usage.DashboardAPIKeysUsage)
		}

		// 公告（用户可见）
		announcements := authenticated.Group("/announcements")
		{
			announcements.GET("", h.Announcement.List)
			announcements.POST("/:id/read", h.Announcement.MarkRead)
		}

		// 卡密兑换
		redeem := authenticated.Group("/redeem")
		{
			redeem.POST("", h.Redeem.Redeem)
			redeem.GET("/history", h.Redeem.GetHistory)
		}

		// 用户订阅
		subscriptions := authenticated.Group("/subscriptions")
		{
			subscriptions.GET("", h.Subscription.List)
			subscriptions.GET("/active", h.Subscription.GetActive)
			subscriptions.GET("/progress", h.Subscription.GetProgress)
			subscriptions.GET("/summary", h.Subscription.GetSummary)
		}

		// 渠道监控（用户只读）
		monitors := authenticated.Group("/channel-monitors")
		{
			monitors.GET("", h.ChannelMonitor.List)
			monitors.GET("/capacity-summary", h.ChannelMonitor.CapacitySummary)
			monitors.GET("/:id/status", h.ChannelMonitor.GetStatus)
		}

		// V2 passive views require feature on + mode=v2.
		monitorV2 := authenticated.Group("/channel-monitor-v2")
		monitorV2.Use(panelRateLimiter.Heavy())
		monitorV2.Use(channelMonitorModeV2Guard(settingService))
		{
			monitorV2.GET("/dimensions", h.ChannelMonitorV2.Dimensions)
			monitorV2.GET("/snapshot", h.ChannelMonitorV2.Snapshot)
			monitorV2.GET("/models", h.ChannelMonitorV2.Models)
			monitorV2.GET("/matrix", h.ChannelMonitorV2.Matrix)
			monitorV2.GET("/errors", h.ChannelMonitorV2.Errors)
			monitorV2.GET("/users", h.ChannelMonitorV2.Users)
		}
	}
}
