package types

import (
	"fmt"
	"regexp"
)

// 驱动身份（driver_id）的命名规则。
//
// 身份是「这个 provider 由哪个驱动实现」的稳定字符串，落库在
// storage_providers.provider_type 里，也出现在插件清单的 driver_id 上。发布后
// 不可更改：改了库里的 provider 就找不到驱动。
//
// 具体有哪些身份由注册方决定：内置驱动在自己的包里注册，插件在 init() 里注册
// （见 RegisterStorageDriverWithMetadata），**本包不认识任何具体驱动**。
var driverIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// ValidateDriverID 校验驱动身份：小写字母/数字/下划线，1~32 字符。
//
// 插件清单（driver_id）与注册表都用它把关，保证身份能安全落库、能进 URL 与日志。
func ValidateDriverID(id string) error {
	if !driverIDPattern.MatchString(id) {
		return fmt.Errorf("非法驱动身份 %q：只允许小写字母、数字、下划线，长度 1~32", id)
	}
	return nil
}
