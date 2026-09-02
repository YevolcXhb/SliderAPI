package provider

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// CreateProvider creates a Provider from a provider key, instance ID and decrypted config.
// 分发经 constructors 注册表（各 provider 的 init() 自注册），新增 provider 只需在
// 自己的文件里 register，不必再改这里。
// 构造器错误一律原样返回（含 wxpay 的 *infraerrors.ApplicationError）：
// "_validate_" 路径依赖该结构化错误做前端 i18n（service/payment_config_providers.go）。
func CreateProvider(providerKey string, instanceID string, config map[string]string) (payment.Provider, error) {
	fn, ok := constructors[providerKey]
	if !ok {
		return nil, fmt.Errorf("unknown provider key: %s", providerKey)
	}
	return fn(instanceID, config)
}
