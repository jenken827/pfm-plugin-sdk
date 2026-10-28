package sdk

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/plugin/sdk/logx"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// hostClient 插件回调宿主的客户端（token 轮换回写、缓存状态、日志）
type hostClient struct {
	addr  string
	token string
}

func (c *hostClient) post(method string, req, resp proto.Message) error {
	body, err := protojson.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequest(http.MethodPost, "http://"+c.addr+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, maxRequestBytes))
	if err != nil {
		return err
	}
	if httpResp.StatusCode != http.StatusOK {
		var rpcErr plugin.Error
		if unmarshalErr := protojson.Unmarshal(raw, &rpcErr); unmarshalErr == nil && rpcErr.GetMessage() != "" {
			return fmt.Errorf("宿主返回 %d: %s", httpResp.StatusCode, rpcErr.GetMessage())
		}
		return fmt.Errorf("宿主返回 %d", httpResp.StatusCode)
	}
	if resp == nil {
		return nil
	}
	return protojson.Unmarshal(raw, resp)
}

// UpdateConfig 实现 types.ProviderConfigPersister：token 轮换后回写 storage_providers.config
func (c *hostClient) UpdateConfig(providerID uint32, configJSON string) error {
	return c.post("/rpc/UpdateProviderConfig", &plugin.UpdateProviderConfigReq{
		ProviderId: proto.Uint32(providerID),
		ConfigJson: proto.String(configJSON),
	}, nil)
}

// UpdateCacheStatus 实现 types.CacheUpdater：更新 file_locations 的缓存状态
func (c *hostClient) UpdateCacheStatus(locationID uint64, status abi.CacheStatus) error {
	return c.post("/rpc/UpdateCacheStatus", &plugin.UpdateCacheStatusReq{
		LocationId: proto.Uint64(locationID),
		Status:     status.Enum(),
	}, nil)
}

// Log 把插件日志送回宿主（插件 stderr 也会被宿主收集，这里是结构化通道）
func (c *hostClient) Log(level, message string) {
	if err := c.post("/rpc/Log", &plugin.LogReq{
		Level:   proto.String(level),
		Message: proto.String(message),
	}, nil); err != nil {
		logx.L().Warnf("plugin log forward failed: %v", err)
	}
}
