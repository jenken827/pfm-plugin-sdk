package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// newTestServer 造一个带两个工具的服务（一个只读、一个可写）
func newTestServer(t *testing.T) (*toolServer, *plugin.CallToolReq) {
	t.Helper()
	tools, err := buildToolRegistry([]Tool{
		{
			Name:        "echo",
			Description: "回显参数",
			InputSchema: `{"type":"object","properties":{"text":{"type":"string"}}}`,
			ReadOnly:    true,
			Call: func(_ context.Context, in *ToolCall) (ToolResult, error) {
				var args struct {
					Text string `json:"text"`
				}
				if err := in.ArgumentsJSON(&args); err != nil {
					return ToolResult{}, err
				}
				return TextResult("echo: " + args.Text), nil
			},
		},
		{
			Name:     "boom",
			ReadOnly: false,
			Call: func(context.Context, *ToolCall) (ToolResult, error) {
				return ErrorResult("故意的失败"), nil
			},
		},
	})
	if err != nil {
		t.Fatalf("buildToolRegistry: %v", err)
	}
	srv := &toolServer{toolID: "demo", tools: tools}
	return srv, &plugin.CallToolReq{Name: strPtr("echo"), ArgumentsJson: strPtr(`{"text":"hi"}`)}
}

func strPtr(s string) *string { return &s }

func TestToolDescribeShape(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/Describe", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp plugin.ToolDescribeResp
	if err := protojson.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.GetToolId() != "demo" || resp.GetAbiVersion() != AbiVersion {
		t.Fatalf("身份/版本不对: %v", &resp)
	}
	if len(resp.GetTools()) != 2 {
		t.Fatalf("工具数 = %d, 想要 2", len(resp.GetTools()))
	}
	first := resp.GetTools()[0]
	if first.GetName() != "echo" || !first.GetReadOnly() || first.GetDestructive() {
		t.Fatalf("第一个工具元数据不对: %v", first)
	}
	if !strings.Contains(first.GetInputSchema(), `"type":"object"`) {
		t.Fatalf("Schema 没透传: %q", first.GetInputSchema())
	}
}

// 没有 Schema 的工具也要下发一个空对象 Schema（客户端都认这个形状）
func TestToolDescribeFillsDefaultSchema(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/Describe", strings.NewReader("{}")))
	var resp plugin.ToolDescribeResp
	if err := protojson.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp.GetTools()[1].GetInputSchema(); got != `{"type":"object","properties":{}}` {
		t.Fatalf("默认 Schema = %q", got)
	}
}

func TestToolCallDispatch(t *testing.T) {
	srv, req := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"name": req.GetName(), "arguments_json": req.GetArgumentsJson()})
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/Call", strings.NewReader(string(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp plugin.CallToolResp
	if err := protojson.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.GetIsError() {
		t.Fatalf("不该是 isError: %v", &resp)
	}
	if len(resp.GetContent()) != 1 || resp.GetContent()[0].GetText() != "echo: hi" {
		t.Fatalf("内容块不对: %v", &resp)
	}
}

// 工具自己报错时走 isError，而不是 RPC 错误
func TestToolCallIsError(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/Call",
		strings.NewReader(`{"name":"boom","arguments_json":"{}"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp plugin.CallToolResp
	if err := protojson.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.GetIsError() || resp.GetContent()[0].GetText() != "故意的失败" {
		t.Fatalf("isError 结果不对: %v", &resp)
	}
}

// 未知工具是协议级错误（宿主会翻译成 MCP 的 -32602）
func TestToolCallUnknownName(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/Call",
		strings.NewReader(`{"name":"nope","arguments_json":"{}"}`)))
	if rec.Code == http.StatusOK {
		t.Fatalf("未知工具不该返回 200: %s", rec.Body.String())
	}
}

func TestToolServeRejectsBadDefinitions(t *testing.T) {
	cases := []struct {
		name  string
		tools []Tool
	}{
		{name: "空清单", tools: nil},
		{name: "名字不合法", tools: []Tool{{Name: "Bad-Name", Call: stubCall}}},
		{name: "没有 Call", tools: []Tool{{Name: "ok"}}},
		{name: "Schema 不是对象", tools: []Tool{{Name: "ok", InputSchema: `[1,2]`, Call: stubCall}}},
		{name: "Schema 不是 JSON", tools: []Tool{{Name: "ok", InputSchema: `{oops`, Call: stubCall}}},
		{name: "重名", tools: []Tool{{Name: "dup", Call: stubCall}, {Name: "dup", Call: stubCall}}},
	}
	for _, tc := range cases {
		if _, err := buildToolRegistry(tc.tools); err == nil {
			t.Fatalf("%s 应该报错", tc.name)
		}
	}
	if err := ServeTool("Bad-ID", Tool{Name: "ok", Call: stubCall}); err == nil {
		t.Fatal("非法 tool_id 应该报错")
	}
}

func stubCall(context.Context, *ToolCall) (ToolResult, error) { return TextResult("ok"), nil }
