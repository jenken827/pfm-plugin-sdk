// Package sdk 把内置驱动包装成独立插件进程的最小运行时。
//
// 用法（插件自己的 main，五行）：
//
//	import (
//		_ "github.com/example/pfm-plugin-driver-baidu/driver/baidu"
//		"github.com/jenken827/pfm-plugin-sdk/plugin/sdk"
//	)
//
//	func main() { sdk.MustServe("baidu") }
//
// 传输约定（与 proto/plugin/driver.proto 对应）：
//   - 启动后向 stdout 打印一行握手：PFM-PLUGIN|1|<127.0.0.1:port>|<pid>
//   - 宿主用一次性 token（环境变量 PFM_PLUGIN_TOKEN）做 Bearer 鉴权
//   - 一元方法：POST /rpc/<Method>，请求/响应体都是 protojson
//   - Open：POST /rpc/Open，响应体是裸字节流（按 offset/limit 裁剪）
//
// 插件与宿主同权限（同一用户、同一文件系统），这是信任模型的前提：
// 只加载可信来源的插件二进制。
package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jenken827/pfm-plugin-sdk/driver/cryptostream"
	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-plugin-sdk/plugin/sdk/logx"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

const (
	// AbiVersion 插件协议版本；直接取契约 proto 里的枚举，不在这里另写一个数字
	AbiVersion = uint32(abi.AbiVersion_ABI_VERSION_1)
	// HandshakePrefix 插件启动后向 stdout 打印的唯一一行握手前缀
	HandshakePrefix = "PFM-PLUGIN|"
	// TokenEnv 宿主通过环境变量下发的一次性握手 token
	TokenEnv = "PFM_PLUGIN_TOKEN"
	// maxRequestBytes 一元请求体上限（Open 的请求也很小）
	maxRequestBytes = 1 << 20
	// streamBufferSize Open 单次下发的数据块大小
	streamBufferSize = 256 << 10
)

// MustServe 以插件身份启动；出错直接退出（宿主会看到进程消失并报错）
func MustServe(driverID string) {
	if err := Serve(driverID); err != nil {
		fmt.Fprintf(os.Stderr, "pfm-plugin(%s): %v\n", driverID, err)
		os.Exit(1)
	}
}

// Serve 启动插件服务，阻塞到监听结束
func Serve(driverID string) error {
	// 先把日志出口接好：插件是独立进程，宿主的 pkg/llog 在这里永远没被 Init 过，
	// 直接用它等于把日志丢进 io.Discard。插件日志写 stderr，宿主会逐行转发到宿主日志。
	logx.Init(driverID)
	types.SetLogger(driverLogger{})

	ctor := types.GetDriverConstructor(driverID)
	if ctor == nil {
		return fmt.Errorf("驱动 %q 未在本进程注册（插件 main 里是否 import 了对应 driver 包？）", driverID)
	}
	srv := &server{
		driverID: driverID,
		ctor:     ctor,
		metadata: types.GetDriverMetadata(driverID),
		token:    os.Getenv(TokenEnv),
		drivers:  make(map[uint32]types.StorageDriver),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen loopback failed: %w", err)
	}
	// 握手必须在下发任何数据之前打印并刷出（os.Stdout 无缓冲）
	fmt.Printf("%s%d|%s|%d\n", HandshakePrefix, AbiVersion, ln.Addr().String(), os.Getpid())
	httpSrv := &http.Server{Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second}
	return httpSrv.Serve(ln)
}

// server 插件侧的驱动容器：一个插件进程可以承载多个 provider 实例
type server struct {
	driverID string
	ctor     types.DriverConstructor
	metadata *abi.DriverInfo
	token    string
	drivers  map[uint32]types.StorageDriver
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/Describe", s.handleDescribe)
	mux.HandleFunc("/rpc/Configure", s.handleConfigure)
	mux.HandleFunc("/rpc/Close", s.handleClose)
	mux.HandleFunc("/rpc/Upload", s.handleUpload)
	mux.HandleFunc("/rpc/Open", s.handleOpen)
	mux.HandleFunc("/rpc/BatchDelete", s.handleBatchDelete)
	mux.HandleFunc("/rpc/ListRemote", s.handleListRemote)
	mux.HandleFunc("/rpc/RemoteRoot", s.handleRemoteRoot)
	mux.HandleFunc("/rpc/DirectLink", s.handleDirectLink)
	mux.HandleFunc("/rpc/PreDownload", s.handlePreDownload)
	mux.HandleFunc("/rpc/Punch", s.handlePunch)
	return s.auth(mux)
}

// auth 校验宿主下发的一次性 token（未设置时不做校验，仅用于本地手测）
func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token 不匹配")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// —— 一元方法 ——

func (s *server) handleDescribe(w http.ResponseWriter, r *http.Request) {
	req := &plugin.DescribeReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.describe(), nil })
}

func (s *server) handleConfigure(w http.ResponseWriter, r *http.Request) {
	req := &plugin.ConfigureReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.configure(req) })
}

func (s *server) handleClose(w http.ResponseWriter, r *http.Request) {
	req := &plugin.CloseReq{}
	s.unary(w, r, req, func() (proto.Message, error) {
		if drv := s.drivers[req.GetProviderId()]; drv != nil {
			if closer, ok := drv.(types.Closer); ok {
				closer.Close()
			}
			delete(s.drivers, req.GetProviderId())
		}
		return &plugin.Empty{}, nil
	})
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	req := &plugin.UploadReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.upload(r.Context(), req) })
}

func (s *server) handleBatchDelete(w http.ResponseWriter, r *http.Request) {
	req := &plugin.BatchDeleteReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.batchDelete(r.Context(), req) })
}

func (s *server) handleListRemote(w http.ResponseWriter, r *http.Request) {
	req := &plugin.ListRemoteReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.listRemote(r.Context(), req) })
}

func (s *server) handleRemoteRoot(w http.ResponseWriter, r *http.Request) {
	req := &plugin.RemoteRootReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.remoteRoot(req) })
}

func (s *server) handleDirectLink(w http.ResponseWriter, r *http.Request) {
	req := &plugin.DirectLinkReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.directLink(r.Context(), req) })
}

func (s *server) handlePreDownload(w http.ResponseWriter, r *http.Request) {
	req := &plugin.PreDownloadReq{}
	s.unary(w, r, req, func() (proto.Message, error) { return s.preDownload(r.Context(), req) })
}

// handlePunch 轻量存活探测
func (s *server) handlePunch(w http.ResponseWriter, r *http.Request) {
	s.unary(w, r, &plugin.PunchReq{}, func() (proto.Message, error) { return &plugin.Empty{}, nil })
}

// unary 统一处理 protojson 编解码与错误映射
func (s *server) unary(w http.ResponseWriter, r *http.Request, req proto.Message,
	call func() (proto.Message, error)) {
	unary(w, r, req, call)
}

// unary 一元方法的公共流程（解析 protojson 请求 → 调实现 → 写响应 / 错误），
// 与具体插件类型无关，驱动与下载器共用。
func unary(w http.ResponseWriter, r *http.Request, req proto.Message,
	call func() (proto.Message, error)) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD", "只接受 POST")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", fmt.Sprintf("读取请求失败: %v", err))
		return
	}
	if err := protojson.Unmarshal(body, req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", fmt.Sprintf("解析请求失败: %v", err))
		return
	}
	resp, err := call()
	if err != nil {
		writeRPCError(w, err)
		return
	}
	writeProto(w, resp)
}

// —— Open：裸字节流 ——

func (s *server) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD", "只接受 POST")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", fmt.Sprintf("读取请求失败: %v", err))
		return
	}
	req := &plugin.OpenReq{}
	if err := protojson.Unmarshal(body, req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", fmt.Sprintf("解析请求失败: %v", err))
		return
	}
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		writeRPCError(w, err)
		return
	}
	encryptor, err := encryptorOf(req.GetEncryption())
	if err != nil {
		writeRPCError(w, err)
		return
	}
	reader, err := drv.Open(r.Context(), fromProtoRefs(req.GetRefs()), types.OpenOptions{Encryptor: encryptor})
	if err != nil {
		writeRPCError(w, err)
		return
	}
	defer func() { _ = reader.Close() }()

	if offset := req.GetOffset(); offset > 0 {
		if _, seekErr := reader.Seek(offset, io.SeekStart); seekErr != nil {
			writeRPCError(w, fmt.Errorf("seek 到 %d 失败: %w", offset, seekErr))
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	var src io.Reader = reader
	if limit := req.GetLimit(); limit > 0 {
		src = io.LimitReader(reader, limit)
	}
	if _, copyErr := io.CopyBuffer(w, src, make([]byte, streamBufferSize)); copyErr != nil {
		// 响应头已发出，只能记录；宿主侧会看到流被截断并自行重试
		logx.L().Warnf("plugin open stream interrupted: provider_id=%d, err=%v", req.GetProviderId(), copyErr)
	}
}

// —— 具体方法实现 ——

func (s *server) describe() *plugin.DescribeResp {
	resp := &plugin.DescribeResp{
		AbiVersion: proto.Uint32(AbiVersion),
		PluginName: proto.String(s.driverID),
		DriverId:   proto.String(s.driverID),
		Metadata:   s.metadata,
	}
	if s.metadata != nil {
		resp.Capabilities = s.metadata.GetCapabilities()
	}
	return resp
}

func (s *server) configure(req *plugin.ConfigureReq) (*plugin.ConfigureResp, error) {
	if req.GetProvider() == nil {
		return nil, newRPCError("INVALID", "configure 缺少 provider")
	}
	providerID := req.GetProviderId()
	driver, err := s.ctor(providerOf(req.GetProvider()))
	if err != nil {
		return nil, err
	}
	if host := req.GetHost(); host.GetAddr() != "" {
		hc := &hostClient{addr: host.GetAddr(), token: host.GetToken()}
		if setter, ok := driver.(types.ConfigPersisterSetter); ok {
			setter.SetConfigPersister(hc)
		}
		if setter, ok := driver.(types.CacheUpdaterSetter); ok {
			setter.SetCacheUpdater(hc)
		}
	}
	s.drivers[providerID] = driver
	return &plugin.ConfigureResp{Capabilities: capabilitiesOf(driver)}, nil
}

func (s *server) upload(ctx context.Context, req *plugin.UploadReq) (*plugin.UploadResp, error) {
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		return nil, err
	}
	encryptor, err := encryptorOf(req.GetEncryption())
	if err != nil {
		return nil, err
	}
	refs, err := drv.Upload(ctx,
		types.UploadSource{
			TempPath: req.GetTempPath(),
			FileName: req.GetFileName(),
			FileSize: req.GetFileSize(),
			FileHash: req.GetFileHash(),
		},
		types.UploadOptions{
			Encryptor:        encryptor,
			ObfuscateName:    req.GetObfuscate(),
			EncryptionSuffix: req.GetEncryptionSuffix(),
		})
	if err != nil {
		return nil, err
	}
	return &plugin.UploadResp{Refs: toProtoRefs(refs)}, nil
}

func (s *server) batchDelete(ctx context.Context, req *plugin.BatchDeleteReq) (*plugin.BatchDeleteResp, error) {
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		return nil, err
	}
	deleter, ok := drv.(types.BatchDeleter)
	if !ok {
		return nil, newRPCError("UNSUPPORTED", "该驱动未实现 BatchDeleter")
	}
	results, err := deleter.BatchDelete(ctx, fromProtoRefs(req.GetRefs()))
	if err != nil && results == nil {
		return nil, err
	}
	resp := &plugin.BatchDeleteResp{}
	for _, result := range results {
		item := &plugin.DeleteResult{
			LocationId: proto.Uint64(result.ID),
			Success:    proto.Bool(result.Success),
			Permanent:  proto.Bool(result.Permanent || types.IsPermanent(result.Error)),
		}
		if result.Error != nil {
			item.Error = proto.String(result.Error.Error())
		}
		resp.Results = append(resp.Results, item)
	}
	return resp, nil
}

func (s *server) listRemote(ctx context.Context, req *plugin.ListRemoteReq) (*plugin.ListRemoteResp, error) {
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		return nil, err
	}
	lister, ok := drv.(types.RemoteLister)
	if !ok {
		return nil, newRPCError("UNSUPPORTED", "该驱动未实现 RemoteLister")
	}
	entries, err := lister.ListRemote(ctx, req.GetDir())
	if err != nil {
		return nil, err
	}
	resp := &plugin.ListRemoteResp{}
	for _, entry := range entries {
		resp.Entries = append(resp.Entries, &plugin.RemoteEntry{
			Path:  proto.String(entry.Path),
			Name:  proto.String(entry.Name),
			Id:    proto.String(entry.ID),
			IsDir: proto.Bool(entry.IsDir),
			Size:  proto.Int64(entry.Size),
		})
	}
	return resp, nil
}

func (s *server) remoteRoot(req *plugin.RemoteRootReq) (*plugin.RemoteRootResp, error) {
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		return nil, err
	}
	lister, ok := drv.(types.RemoteLister)
	if !ok {
		return nil, newRPCError("UNSUPPORTED", "该驱动未实现 RemoteLister")
	}
	return &plugin.RemoteRootResp{Path: proto.String(lister.RemoteRoot())}, nil
}

func (s *server) directLink(ctx context.Context, req *plugin.DirectLinkReq) (*plugin.DirectLinkResp, error) {
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		return nil, err
	}
	linker, ok := drv.(types.DirectLinker)
	if !ok {
		return nil, newRPCError("UNSUPPORTED", "该驱动未实现 DirectLinker")
	}
	if req.GetRef() == nil {
		return nil, newRPCError("INVALID", "direct_link 缺少 ref")
	}
	link, err := linker.DirectLink(ctx, fromProtoRef(req.GetRef()))
	if err != nil {
		return nil, err
	}
	resp := &plugin.DirectLinkResp{
		Url:           proto.String(link.URL),
		Headers:       link.Headers,
		SupportsRange: proto.Bool(link.SupportsRange),
	}
	if !link.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(link.ExpiresAt)
	}
	return resp, nil
}

func (s *server) preDownload(ctx context.Context, req *plugin.PreDownloadReq) (*plugin.Empty, error) {
	drv, err := s.driver(req.GetProviderId())
	if err != nil {
		return nil, err
	}
	pre, ok := drv.(types.PreDownloader)
	if !ok {
		return nil, newRPCError("UNSUPPORTED", "该驱动未实现 PreDownloader")
	}
	if err := pre.PreDownload(ctx, fromProtoRefs(req.GetRefs())); err != nil {
		return nil, err
	}
	return &plugin.Empty{}, nil
}

// —— 工具 ——

func (s *server) driver(providerID uint32) (types.StorageDriver, error) {
	drv := s.drivers[providerID]
	if drv == nil {
		return nil, newRPCError("NOT_CONFIGURED", fmt.Sprintf("provider %d 尚未 Configure", providerID))
	}
	return drv, nil
}

// capabilitiesOf 按实例实际实现的接口得出能力集合
func capabilitiesOf(drv types.StorageDriver) []abi.DriverCapability {
	var caps []abi.DriverCapability
	if _, ok := drv.(types.BatchDeleter); ok {
		caps = append(caps, abi.DriverCapability_DRIVER_CAPABILITY_BATCH_DELETE)
	}
	if _, ok := drv.(types.DirectLinker); ok {
		caps = append(caps, abi.DriverCapability_DRIVER_CAPABILITY_DIRECT_LINK)
	}
	if _, ok := drv.(types.RemoteLister); ok {
		caps = append(caps, abi.DriverCapability_DRIVER_CAPABILITY_REMOTE_LIST)
	}
	if _, ok := drv.(types.PreDownloader); ok {
		caps = append(caps, abi.DriverCapability_DRIVER_CAPABILITY_PREDOWNLOAD)
	}
	return caps
}

func encryptorOf(enc *plugin.Encryption) (types.Encryptor, error) {
	if enc.GetKey() == "" {
		return nil, nil
	}
	return cryptostream.NewCryptEncryptor(enc.GetKey(), enc.GetSalt())
}

// rpcError 带错误码的插件侧错误（宿主据此区分可重试与不可重试）
type rpcError struct {
	code    string
	message string
}

func (e *rpcError) Error() string { return e.code + ": " + e.message }

func newRPCError(code, message string) *rpcError { return &rpcError{code: code, message: message} }

func writeRPCError(w http.ResponseWriter, err error) {
	var rpcErr *rpcError
	if errors.As(err, &rpcErr) {
		writeError(w, statusForCode(rpcErr.code), rpcErr.code, rpcErr.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
}

func statusForCode(code string) int {
	switch code {
	case "INVALID", "NOT_CONFIGURED":
		return http.StatusBadRequest
	case "UNSUPPORTED":
		return http.StatusNotImplemented
	case "AUTH_EXPIRED":
		return http.StatusUnauthorized
	case "TRANSIENT", "RATE_LIMITED":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeProtoWithStatus(w, status, &plugin.Error{Code: proto.String(code), Message: proto.String(message)})
}

func writeProto(w http.ResponseWriter, msg proto.Message) {
	writeProtoWithStatus(w, http.StatusOK, msg)
}

func writeProtoWithStatus(w http.ResponseWriter, status int, msg proto.Message) {
	body, err := protojson.Marshal(msg)
	if err != nil {
		http.Error(w, `{"code":"INTERNAL","message":"marshal failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
