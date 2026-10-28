package sdk

// 指针工具的插件侧版本
//
// 驱动元数据（DriverInfo / ConfigField）大量用 *string / *bool，宿主侧的
// utils.StringPtr 在同一个包里还依赖 globals 与 internal/config —— 插件只为了
// 造几个指针就把它整包链进来并不划算，所以在 SDK 里给一份等价实现。

// StringPtr 字符串指针
func StringPtr(s string) *string { return &s }

// BoolPtr 布尔指针
func BoolPtr(b bool) *bool { return &b }

// Uint32Ptr uint32 指针
func Uint32Ptr(u uint32) *uint32 { return &u }

// Uint64Ptr uint64 指针
func Uint64Ptr(u uint64) *uint64 { return &u }
