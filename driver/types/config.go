package types

import (
	"sort"
	"sync"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
)

// DriverRegistry 驱动注册器
type DriverRegistry struct {
	constructor DriverConstructor
	metadata    *abi.DriverInfo
}

// DriverConstructor 驱动构造函数
//
// 入参是驱动视角的 Provider（身份 + 不透明配置 JSON + 提供者级配置视图），
// 不是宿主的 ORM / proto 实体：驱动库与插件进程都不该为构造一个实例而链接宿主的
// 数据模型（见 Provider 的注释）。
type DriverConstructor func(provider *Provider) (StorageDriver, error)

// driverRegistries 驱动注册表（内置驱动在 init 阶段注册，之后只读）
var driverRegistries = make(map[string]*DriverRegistry)

// driverOverrides 插件覆盖层：插件可为已存在的驱动身份提供替代实现。
//
// 与 driverRegistries 分开存放，保证「插件加载失败 / 被禁用 / 关闭热加载」时
// 仍能回退到内置实现；GetDriverConstructor / GetDriverMetadata 优先读覆盖层。
var (
	driverOverridesMu sync.RWMutex
	driverOverrides   = make(map[string]*DriverRegistry)
)

// RegisterStorageDriverWithMetadata 注册存储驱动（带元数据）
//
// metadata.DriverId 即驱动身份，必须通过 ValidateDriverID；重复注册同一身份
// 会覆盖前一次注册（插件覆盖层请用 SetDriverOverride，不要走这里）。
func RegisterStorageDriverWithMetadata(constructor DriverConstructor, metadata *abi.DriverInfo) {
	if metadata == nil || metadata.DriverId == nil {
		return
	}
	driverRegistries[*metadata.DriverId] = &DriverRegistry{
		constructor: constructor,
		metadata:    metadata,
	}
}

// SetDriverOverride 用插件实现覆盖某个驱动身份的构造器与元数据
//
// 内置注册保持不变，可随时回退（ClearDriverOverride）。
func SetDriverOverride(constructor DriverConstructor, metadata *abi.DriverInfo) {
	if constructor == nil || metadata == nil || metadata.DriverId == nil {
		return
	}
	driverOverridesMu.Lock()
	defer driverOverridesMu.Unlock()
	driverOverrides[*metadata.DriverId] = &DriverRegistry{
		constructor: constructor,
		metadata:    metadata,
	}
}

// ClearDriverOverride 撤销插件覆盖，回退到内置实现
func ClearDriverOverride(driverID string) {
	driverOverridesMu.Lock()
	defer driverOverridesMu.Unlock()
	delete(driverOverrides, driverID)
}

// HasDriverOverride 判断某个身份当前是否由插件实现
func HasDriverOverride(driverID string) bool {
	driverOverridesMu.RLock()
	defer driverOverridesMu.RUnlock()
	_, ok := driverOverrides[driverID]
	return ok
}

// getDriverOverride 取覆盖层注册（无覆盖返回 nil）
func getDriverOverride(driverID string) *DriverRegistry {
	driverOverridesMu.RLock()
	defer driverOverridesMu.RUnlock()
	return driverOverrides[driverID]
}

// GetDriverMetadata 获取驱动元数据（插件覆盖优先，未知身份返回 nil）
func GetDriverMetadata(driverID string) *abi.DriverInfo {
	if override := getDriverOverride(driverID); override != nil {
		return override.metadata
	}
	if registry, ok := driverRegistries[driverID]; ok {
		return registry.metadata
	}
	return nil
}

// GetAllDriverMetadata 获取所有可用驱动元数据（内置 + 插件新身份，插件覆盖优先）
func GetAllDriverMetadata() []*abi.DriverInfo {
	driverOverridesMu.RLock()
	overrides := make(map[string]*abi.DriverInfo, len(driverOverrides))
	for driverID, registry := range driverOverrides {
		overrides[driverID] = registry.metadata
	}
	driverOverridesMu.RUnlock()

	metadatas := make([]*abi.DriverInfo, 0, len(driverRegistries)+len(overrides))
	for driverID, registry := range driverRegistries {
		if override, ok := overrides[driverID]; ok {
			metadatas = append(metadatas, override)
			continue
		}
		metadatas = append(metadatas, registry.metadata)
	}
	// 插件新增的身份（内置注册里没有）也要进列表，否则前端下拉框看不到
	for driverID, metadata := range overrides {
		if _, ok := driverRegistries[driverID]; !ok {
			metadatas = append(metadatas, metadata)
		}
	}
	// map 遍历顺序每次都不一样，这里只保证**确定性**（按 driver_id 字典序），
	// 展示顺序是调用方的事：宿主按自己的内置驱动清单排在前，其余按字典序。
	sort.Slice(metadatas, func(i, j int) bool {
		return metadatas[i].GetDriverId() < metadatas[j].GetDriverId()
	})
	return metadatas
}

// GetDriverConstructor 获取驱动构造函数（插件覆盖优先，未知身份返回 nil）
func GetDriverConstructor(driverID string) DriverConstructor {
	if override := getDriverOverride(driverID); override != nil {
		return override.constructor
	}
	if registry, ok := driverRegistries[driverID]; ok {
		return registry.constructor
	}
	return nil
}
