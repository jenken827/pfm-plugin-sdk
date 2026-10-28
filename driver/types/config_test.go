package types

import (
	"context"
	"testing"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
)

// 测试用驱动身份：避开真实驱动，避免污染其他用例
const testOverrideType = "test_override"

func newTestCtor(name string) DriverConstructor {
	return func(*Provider) (StorageDriver, error) {
		return &namedTestDriver{name: name}, nil
	}
}

type namedTestDriver struct{ name string }

func (d *namedTestDriver) GetType() string                    { return testOverrideType }
func (d *namedTestDriver) GetName() string                    { return d.name }
func (d *namedTestDriver) GetConfig() any                     { return nil }
func (d *namedTestDriver) GetProviderConfig() *ProviderConfig { return &ProviderConfig{} }
func (d *namedTestDriver) Upload(
	_ context.Context, _ UploadSource, _ UploadOptions,
) ([]FileRef, error) {
	return nil, nil
}
func (d *namedTestDriver) Open(
	_ context.Context, _ []FileRef, _ OpenOptions,
) (ReadSeekCloser, error) {
	return nil, nil
}

func metadataFor(name string) *abi.DriverInfo {
	id := testOverrideType
	return &abi.DriverInfo{DriverId: &id, Name: &name}
}

// 覆盖层：写入后可读到插件构造器与元数据，Clear 之后回退内置实现
func TestDriverOverride(t *testing.T) {
	RegisterStorageDriverWithMetadata(newTestCtor("builtin"), metadataFor("builtin"))
	t.Cleanup(func() { ClearDriverOverride(testOverrideType) })

	if HasDriverOverride(testOverrideType) {
		t.Fatal("初始状态不应存在覆盖")
	}
	if GetDriverMetadata(testOverrideType).GetName() != "builtin" {
		t.Fatalf("内置元数据异常: %v", GetDriverMetadata(testOverrideType).GetName())
	}

	SetDriverOverride(newTestCtor("plugin"), metadataFor("plugin"))
	if !HasDriverOverride(testOverrideType) {
		t.Fatal("设置覆盖后 HasDriverOverride 应为 true")
	}
	if got := GetDriverMetadata(testOverrideType).GetName(); got != "plugin" {
		t.Fatalf("覆盖后元数据应来自插件，实际 %s", got)
	}
	drv, err := GetDriverConstructor(testOverrideType)(nil)
	if err != nil {
		t.Fatalf("覆盖后构造驱动失败: %v", err)
	}
	if drv.GetName() != "plugin" {
		t.Fatalf("覆盖后构造器应来自插件，实际 %s", drv.GetName())
	}

	ClearDriverOverride(testOverrideType)
	if HasDriverOverride(testOverrideType) {
		t.Fatal("撤销覆盖后 HasDriverOverride 应为 false")
	}
	if got := GetDriverMetadata(testOverrideType).GetName(); got != "builtin" {
		t.Fatalf("撤销覆盖后应回退内置元数据，实际 %s", got)
	}
	drv, err = GetDriverConstructor(testOverrideType)(nil)
	if err != nil {
		t.Fatalf("撤销覆盖后构造驱动失败: %v", err)
	}
	if drv.GetName() != "builtin" {
		t.Fatalf("撤销覆盖后构造器应回退内置，实际 %s", drv.GetName())
	}
}

// 元数据缺 DriverId 时不允许注册/覆盖（身份是唯一键，不能为空）
func TestDriverOverrideIgnoresInvalidMetadata(t *testing.T) {
	SetDriverOverride(newTestCtor("plugin"), &abi.DriverInfo{})
	if HasDriverOverride("") {
		t.Fatal("空 DriverId 不应写入覆盖层")
	}
	if ctor := GetDriverConstructor(""); ctor != nil {
		t.Fatal("空 DriverId 不应查到构造器")
	}
}

// 身份校验：只允许小写字母/数字/下划线，1~32 字符
func TestValidateDriverID(t *testing.T) {
	for _, ok := range []string{"local", "baidu", "aliyundrive_open", "a", "x1_y2"} {
		if err := ValidateDriverID(ok); err != nil {
			t.Fatalf("%q 应合法: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Baidu", "baidu-x", "百度", "a b", "this_id_is_way_too_long_for_the_limit"} {
		if err := ValidateDriverID(bad); err == nil {
			t.Fatalf("%q 应判为非法", bad)
		}
	}
}

// 能力判定只看 DriverInfo.Capabilities
func TestHasCapability(t *testing.T) {
	id := "example_driver"
	md := &abi.DriverInfo{DriverId: &id, Capabilities: []abi.DriverCapability{
		abi.DriverCapability_DRIVER_CAPABILITY_BATCH_DELETE,
		abi.DriverCapability_DRIVER_CAPABILITY_DIRECT_LINK,
	}}
	if !HasCapability(md, abi.DriverCapability_DRIVER_CAPABILITY_DIRECT_LINK) {
		t.Fatal("声明过的能力应返回 true")
	}
	if HasCapability(md, abi.DriverCapability_DRIVER_CAPABILITY_REMOTE_LIST) {
		t.Fatal("未声明的能力应返回 false")
	}
	if HasCapability(nil, abi.DriverCapability_DRIVER_CAPABILITY_DIRECT_LINK) {
		t.Fatal("nil 元数据应返回 false")
	}
}

// 列表顺序只保证确定性（按 driver_id 字典序），不含"内置驱动排前面"这类展示语义
func TestGetAllDriverMetadataDeterministic(t *testing.T) {
	ids := []string{"zeta_driver", "alpha_driver"}
	for _, id := range ids {
		driverID := id
		RegisterStorageDriverWithMetadata(
			func(*Provider) (StorageDriver, error) { return nil, nil },
			&abi.DriverInfo{DriverId: &driverID},
		)
	}
	first := GetAllDriverMetadata()
	for i := 1; i < len(first); i++ {
		if first[i-1].GetDriverId() > first[i].GetDriverId() {
			t.Fatalf("列表未按 driver_id 字典序：%q 在 %q 之前",
				first[i-1].GetDriverId(), first[i].GetDriverId())
		}
	}
	second := GetAllDriverMetadata()
	if len(second) != len(first) {
		t.Fatalf("两次调用数量不一致：%d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].GetDriverId() != second[i].GetDriverId() {
			t.Fatalf("第 %d 项两次调用不一致：%q vs %q", i, first[i].GetDriverId(), second[i].GetDriverId())
		}
	}
}
