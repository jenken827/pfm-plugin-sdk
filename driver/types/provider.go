package types

import "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"

// Provider 驱动实例的宿主上下文
//
// 只包含驱动真正需要的东西：身份、用户起的名、**该驱动私有的配置 JSON**，
// 以及宿主算好的提供者级配置视图（加密、限速、hash 类型等）。
//
// 刻意不暴露宿主的数据模型（GORM 实体 / proto 实体）：驱动既不该被
// deleted_at、proxy、total_capacity 这类与它无关的字段污染，也不该为了读一个
// provider id 就把宿主的 ORM 与 gorm 链接进插件进程。驱动自己的配置结构由驱动
// 自己定义、自己演进（宿主只当不透明 JSON 字符串存取）。
type Provider struct {
	// ID 存储提供者主键（token 回写、缓存状态更新等回调要用）
	ID uint32
	// DriverID 驱动身份（如 local / baidu）
	DriverID string
	// Name 用户起的实例名（仅用于日志与报错）
	Name string
	// ConfigJSON 该驱动的私有配置（不透明 JSON，结构由驱动自行定义与解析）
	ConfigJSON string
	// Config 宿主视角的提供者级配置
	Config ProviderConfig
	// Proxy 该提供者配置的 HTTP 代理（可为空）；网络类驱动建 client 时用它
	Proxy string
}

// ProviderConfigOf 从 ABI 的 Provider 消息取出提供者级配置视图（插件 SDK 侧用）
func ProviderConfigOf(p *abi.Provider) ProviderConfig {
	if p == nil {
		return ProviderConfig{}
	}
	return ProviderConfig{
		EncryptionKey:        p.GetEncryptionKey(),
		EncryptionSuffix:     p.GetEncryptionSuffix(),
		SortOrder:            p.GetSortOrder(),
		FileSizeLimit:        p.GetFileSizeLimit(),
		MaxConcurrentUploads: p.GetMaxConcurrentUploads(),
		HashType:             p.GetHashType(),
		Enabled:              p.GetEnabled(),
	}
}
