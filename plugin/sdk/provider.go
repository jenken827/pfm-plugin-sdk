package sdk

import (
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-plugin-sdk/plugin/sdk/logx"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
)

// driverLogger 把 driver/types 的日志接口接到插件日志（stderr）
type driverLogger struct{}

func (driverLogger) Debugf(format string, args ...any) { logx.L().Debugf(format, args...) }
func (driverLogger) Infof(format string, args ...any)  { logx.L().Infof(format, args...) }
func (driverLogger) Warnf(format string, args ...any)  { logx.L().Warnf(format, args...) }
func (driverLogger) Errorf(format string, args ...any) { logx.L().Errorf(format, args...) }

// providerOf 把插件协议里的 provider 消息转成驱动视角的 Provider
//
// 驱动只拿它需要的部分：身份、不透明配置 JSON、提供者级配置视图（含 proxy）。
// 宿主的数据实体（时间戳、容量、统计…）不进驱动，驱动端也就不必链接宿主的数据模型。
func providerOf(pb *abi.Provider) *types.Provider {
	if pb == nil {
		return &types.Provider{}
	}
	return &types.Provider{
		ID:         pb.GetId(),
		DriverID:   pb.GetDriverId(),
		Name:       pb.GetName(),
		ConfigJSON: pb.GetConfig(),
		Config:     types.ProviderConfigOf(pb),
		Proxy:      pb.GetProxy(),
	}
}
