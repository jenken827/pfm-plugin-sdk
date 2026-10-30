package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// Tool 一个工具的定义与实现（kind=tool 插件用）。
//
// 插件只负责「工具清单 + 执行逻辑」：MCP 的 HTTP 入口、协议协商、令牌 scope、
// 写开关与审计都在宿主核心，插件看不到客户端与令牌。
type Tool struct {
	// Name 工具名（不带 tool_id 前缀），在 MCP 里暴露为 <tool_id>.<name>
	Name string
	// Title 展示名（可空，客户端没有就显示 Name）
	Title string
	// Description 给模型看的说明：做什么、什么时候用、返回什么
	Description string
	// InputSchema 入参 JSON Schema（对象根）；空表示无参数
	InputSchema string
	// ReadOnly 只读工具；false 时宿主按「写开关 + 令牌写权限」过滤
	ReadOnly bool
	// Destructive 破坏性操作（删除 / 覆盖），客户端据此提示用户
	Destructive bool
	// Idempotent 重复调用结果一致
	Idempotent bool
	// Call 执行入口；返回 error 时宿主给客户端一个 isError 结果
	Call func(ctx context.Context, in *ToolCall) (ToolResult, error)
}

// ToolCall 一次工具调用的入参。
type ToolCall struct {
	// Arguments 参数原文（JSON 对象文本，来自 MCP tools/call 的 arguments）
	Arguments []byte
	// ArgumentsJSON 解析参数；结构体字段用 json tag 对齐 InputSchema
	ArgumentsJSON func(v any) error
	// Caller 调用方信息（用户 / 令牌 / 可见根目录），只读
	Caller *plugin.ToolCaller
	// ReadFile 读文件字节（是否需要以 Caller.RootFolderId 为视角由插件自己决定，
	// 宿主按调用方的真实 scope 再校验一次，越权直接报错）
	ReadFile func(ctx context.Context, fileID string, offset uint64, maxBytes uint32) (*plugin.ReadFileResp, error)
}

// ToolResult 一次工具调用的结果。
type ToolResult struct {
	Content []*plugin.ToolContent
	IsError bool
}

// TextResult 纯文本结果
func TextResult(text string) ToolResult {
	return ToolResult{Content: []*plugin.ToolContent{{
		Type: plugin.ToolContentType_TOOL_CONTENT_TYPE_TEXT.Enum(),
		Text: proto.String(text),
	}}}
}

// ErrorResult 工具级失败（客户端看到 isError，而不是协议错误）
func ErrorResult(format string, args ...any) ToolResult {
	return ToolResult{IsError: true, Content: []*plugin.ToolContent{{
		Type: plugin.ToolContentType_TOOL_CONTENT_TYPE_TEXT.Enum(),
		Text: proto.String(fmt.Sprintf(format, args...)),
	}}}
}

// ImageResult 图片结果（data 为原始字节，protojson 里自动 base64）
func ImageResult(data []byte, mimeType string) ToolResult {
	return ToolResult{Content: []*plugin.ToolContent{{
		Type:     plugin.ToolContentType_TOOL_CONTENT_TYPE_IMAGE.Enum(),
		Data:     data,
		MimeType: proto.String(mimeType),
	}}}
}

// toolIDPattern 插件身份与工具名的命名规则（与 plugin.json 的校验一致）
var toolIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// MustServeTool 以 kind=tool 插件身份启动；出错直接退出
func MustServeTool(toolID string, tools ...Tool) {
	if err := ServeTool(toolID, tools...); err != nil {
		fmt.Fprintf(os.Stderr, "pfm-plugin(%s): %v\n", toolID, err)
		os.Exit(1)
	}
}

// ServeTool 启动工具插件服务，阻塞到监听结束。
//
// 传输约定与其他 kind 一致（握手行 / Bearer token / POST /rpc/<Method> + protojson）。
// 工具清单在启动时就校验（名字重复 / Schema 不是 JSON 对象都直接失败），避免宿主
// 拿到一份半可用的工具表。
func ServeTool(toolID string, tools ...Tool) error {
	if !toolIDPattern.MatchString(toolID) {
		return fmt.Errorf("tool_id %q 不合法：小写字母开头，只含小写字母 / 数字 / 下划线", toolID)
	}
	registry, err := buildToolRegistry(tools)
	if err != nil {
		return err
	}
	srv := &toolServer{
		toolID: toolID,
		tools:  registry,
		token:  os.Getenv(TokenEnv),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen loopback failed: %w", err)
	}
	fmt.Printf("%s%d|%s|%d\n", HandshakePrefix, AbiVersion, ln.Addr().String(), os.Getpid())
	httpSrv := &http.Server{Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second}
	return httpSrv.Serve(ln)
}

// buildToolRegistry 校验并索引工具定义（保持声明顺序，便于 Describe 稳定输出）
func buildToolRegistry(tools []Tool) ([]Tool, error) {
	if len(tools) == 0 {
		return nil, fmt.Errorf("至少需要一个工具")
	}
	seen := make(map[string]struct{}, len(tools))
	for i, tool := range tools {
		if !toolIDPattern.MatchString(tool.Name) {
			return nil, fmt.Errorf("第 %d 个工具名 %q 不合法", i+1, tool.Name)
		}
		if _, dup := seen[tool.Name]; dup {
			return nil, fmt.Errorf("工具名 %q 重复", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		if tool.Call == nil {
			return nil, fmt.Errorf("工具 %q 没有 Call", tool.Name)
		}
		if err := validateInputSchema(tool); err != nil {
			return nil, err
		}
	}
	return tools, nil
}

// validateInputSchema 入参 Schema 必须是一段 JSON 对象文本（空表示无参数）
func validateInputSchema(tool Tool) error {
	if tool.InputSchema == "" {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(tool.InputSchema), &schema); err != nil {
		return fmt.Errorf("工具 %q 的 InputSchema 不是合法 JSON：%w", tool.Name, err)
	}
	if kind, _ := schema["type"].(string); kind != "object" {
		return fmt.Errorf("工具 %q 的 InputSchema 必须以 \"type\": \"object\" 为根", tool.Name)
	}
	return nil
}

// toolServer 工具插件的 HTTP 服务（路由表与 proto 的方法名一一对应）
type toolServer struct {
	toolID string
	tools  []Tool
	token  string
}

func (s *toolServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/Describe", s.handleDescribe)
	mux.HandleFunc("/rpc/Call", s.handleCall)
	return s.auth(mux)
}

func (s *toolServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token 不匹配")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *toolServer) handleDescribe(w http.ResponseWriter, r *http.Request) {
	req := &plugin.DescribeReq{}
	unary(w, r, req, func() (proto.Message, error) {
		specs := make([]*plugin.ToolSpec, 0, len(s.tools))
		for _, tool := range s.tools {
			specs = append(specs, &plugin.ToolSpec{
				Name:        proto.String(tool.Name),
				Title:       optionalString(tool.Title),
				Description: optionalString(tool.Description),
				InputSchema: proto.String(defaultSchema(tool.InputSchema)),
				ReadOnly:    proto.Bool(tool.ReadOnly),
				Destructive: proto.Bool(tool.Destructive),
				Idempotent:  proto.Bool(tool.Idempotent),
			})
		}
		return &plugin.ToolDescribeResp{
			AbiVersion: proto.Uint32(AbiVersion),
			PluginName: proto.String(s.toolID),
			ToolId:     proto.String(s.toolID),
			Tools:      specs,
		}, nil
	})
}

func (s *toolServer) handleCall(w http.ResponseWriter, r *http.Request) {
	req := &plugin.CallToolReq{}
	unary(w, r, req, func() (proto.Message, error) {
		tool, ok := s.lookup(req.GetName())
		if !ok {
			return nil, newRPCError("INVALID", fmt.Sprintf("插件 %s 没有工具 %q", s.toolID, req.GetName()))
		}
		rawArgs := argumentsBytes(req.GetArgumentsJson())
		call := &ToolCall{
			Arguments:     rawArgs,
			ArgumentsJSON: func(v any) error { return json.Unmarshal(rawArgs, v) },
			Caller:        req.GetCaller(),
			ReadFile:      readFileFunc(req.GetHost(), req.GetCaller()),
		}
		result, err := tool.Call(r.Context(), call)
		if err != nil {
			return nil, err
		}
		return &plugin.CallToolResp{Content: result.Content, IsError: proto.Bool(result.IsError)}, nil
	})
}

// lookup 按名字找工具（线性查找，工具数是个位数）
func (s *toolServer) lookup(name string) (Tool, bool) {
	for _, tool := range s.tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

// readFileFunc 把宿主回调包成闭包；没有 host 端点时返回明确错误而不是静默失败
func readFileFunc(host *plugin.HostEndpoint, caller *plugin.ToolCaller) func(
	context.Context, string, uint64, uint32) (*plugin.ReadFileResp, error) {
	return func(ctx context.Context, fileID string, offset uint64, maxBytes uint32) (*plugin.ReadFileResp, error) {
		if host.GetAddr() == "" {
			return nil, fmt.Errorf("宿主没有下发回调地址，无法读取文件")
		}
		resp := &plugin.ReadFileResp{}
		client := &hostClient{addr: host.GetAddr(), token: host.GetToken()}
		if err := client.post("/rpc/ReadFile", &plugin.ReadFileReq{
			AccessToken: proto.String(caller.GetAccessToken()),
			FileId:      proto.String(fileID),
			Offset:      proto.Uint64(offset),
			MaxBytes:    proto.Uint32(maxBytes),
		}, resp); err != nil {
			return nil, err
		}
		return resp, nil
	}
}

// optionalString 空串不下发（保持 protojson 输出干净）
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}

// defaultSchema 没有参数的工具也要给一个空对象 Schema，客户端都认这个形状
func defaultSchema(schema string) string {
	if schema == "" {
		return `{"type":"object","properties":{}}`
	}
	return schema
}

// argumentsBytes 空参数按空对象处理（插件侧解析更省事）
func argumentsBytes(raw string) []byte {
	if raw == "" {
		return []byte("{}")
	}
	return []byte(raw)
}
