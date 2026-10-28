package types

// ProviderConfigPersister 存储提供者配置持久化器
// 驱动通过此接口持久化刷新后的配置（如 OAuth token），而不是直接 import internal/db
type ProviderConfigPersister interface {
	// UpdateConfig 持久化存储提供者的配置 JSON
	UpdateConfig(providerID uint32, configJSON string) error
}

// ConfigPersisterSetter 可注入配置持久化器的驱动（可选接口）
type ConfigPersisterSetter interface {
	SetConfigPersister(p ProviderConfigPersister)
}
