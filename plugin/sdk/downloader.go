package sdk

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
)

// Downloader 离线下载后端接口（kind=downloader 插件实现它）。
//
// 插件只负责对接真正的下载器（aria2 / qBittorrent …）：提交任务、查进度、取消；
// 产物文件落在宿主机上，完成后的入库由宿主做，插件不要碰 pfm 的数据库与存储。
type Downloader interface {
	// Configure 初始化后端连接；options 原样来自宿主配置 downloader.options。
	// 返回后端版本号（如 aria2 1.37.0），宿主只用来打日志。
	Configure(ctx context.Context, options map[string]string) (backendVersion string, err error)
	// AddURI 按 URL 列表提交任务，返回任务 id
	AddURI(ctx context.Context, uris []string, options map[string]string) (gid string, err error)
	// TellStatus 查单个任务
	TellStatus(ctx context.Context, gid string) (*plugin.DownloadTask, error)
	// ListTasks 按状态列任务；states 为空表示全部
	ListTasks(ctx context.Context, states []plugin.DownloadStatus) ([]*plugin.DownloadTask, error)
	// Remove 停止任务（保留结果）
	Remove(ctx context.Context, gid string) error
	// RemoveResult 删除任务结果
	RemoveResult(ctx context.Context, gid string) error
	// Version 后端版本号
	Version(ctx context.Context) (string, error)
}

// MustServeDownloader 以 kind=downloader 插件身份启动；出错直接退出
func MustServeDownloader(downloaderID string, impl Downloader) {
	if err := ServeDownloader(downloaderID, impl); err != nil {
		fmt.Fprintf(os.Stderr, "pfm-plugin(%s): %v\n", downloaderID, err)
		os.Exit(1)
	}
}

// ServeDownloader 启动下载器插件服务，阻塞到监听结束。
//
// 传输约定与驱动插件一致（握手行 / Bearer token / POST /rpc/<Method> + protojson），
// 只是方法集换成 DownloaderPlugin 那一套。
func ServeDownloader(downloaderID string, impl Downloader) error {
	if impl == nil {
		return fmt.Errorf("下载器实现为空")
	}
	srv := &downloaderServer{
		downloaderID: downloaderID,
		impl:         impl,
		token:        os.Getenv(TokenEnv),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen loopback failed: %w", err)
	}
	fmt.Printf("%s%d|%s|%d\n", HandshakePrefix, AbiVersion, ln.Addr().String(), os.Getpid())
	httpSrv := &http.Server{Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second}
	return httpSrv.Serve(ln)
}

// downloaderServer 下载器插件的 HTTP 服务（路由表与 proto 的方法名一一对应）
type downloaderServer struct {
	downloaderID string
	impl         Downloader
	token        string
}

func (s *downloaderServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/Describe", s.handleDescribe)
	mux.HandleFunc("/rpc/Configure", s.handleConfigure)
	mux.HandleFunc("/rpc/AddDownload", s.handleAdd)
	mux.HandleFunc("/rpc/TellStatus", s.handleTellStatus)
	mux.HandleFunc("/rpc/ListTasks", s.handleListTasks)
	mux.HandleFunc("/rpc/Remove", s.handleRemove)
	mux.HandleFunc("/rpc/RemoveResult", s.handleRemoveResult)
	mux.HandleFunc("/rpc/Version", s.handleVersion)
	return s.auth(mux)
}

func (s *downloaderServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "token 不匹配")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *downloaderServer) handleDescribe(w http.ResponseWriter, r *http.Request) {
	req := &plugin.DescribeReq{}
	unary(w, r, req, func() (proto.Message, error) {
		return &plugin.DownloaderDescribeResp{
			AbiVersion:   proto.Uint32(AbiVersion),
			PluginName:   proto.String(s.downloaderID),
			DownloaderId: proto.String(s.downloaderID),
		}, nil
	})
}

func (s *downloaderServer) handleConfigure(w http.ResponseWriter, r *http.Request) {
	req := &plugin.DownloaderConfigureReq{}
	unary(w, r, req, func() (proto.Message, error) {
		version, err := s.impl.Configure(r.Context(), req.GetOptions())
		if err != nil {
			return nil, err
		}
		return &plugin.DownloaderConfigureResp{BackendVersion: proto.String(version)}, nil
	})
}

func (s *downloaderServer) handleAdd(w http.ResponseWriter, r *http.Request) {
	req := &plugin.AddDownloadReq{}
	unary(w, r, req, func() (proto.Message, error) {
		gid, err := s.impl.AddURI(r.Context(), req.GetUris(), req.GetOptions())
		if err != nil {
			return nil, err
		}
		return &plugin.AddDownloadResp{Gid: proto.String(gid)}, nil
	})
}

func (s *downloaderServer) handleTellStatus(w http.ResponseWriter, r *http.Request) {
	req := &plugin.TellStatusReq{}
	unary(w, r, req, func() (proto.Message, error) {
		task, err := s.impl.TellStatus(r.Context(), req.GetGid())
		if err != nil {
			return nil, err
		}
		return &plugin.TellStatusResp{Task: task}, nil
	})
}

func (s *downloaderServer) handleListTasks(w http.ResponseWriter, r *http.Request) {
	req := &plugin.ListTasksReq{}
	unary(w, r, req, func() (proto.Message, error) {
		tasks, err := s.impl.ListTasks(r.Context(), req.GetStates())
		if err != nil {
			return nil, err
		}
		return &plugin.ListTasksResp{Tasks: tasks}, nil
	})
}

func (s *downloaderServer) handleRemove(w http.ResponseWriter, r *http.Request) {
	req := &plugin.RemoveTaskReq{}
	unary(w, r, req, func() (proto.Message, error) {
		return &plugin.RemoveTaskResp{}, s.impl.Remove(r.Context(), req.GetGid())
	})
}

func (s *downloaderServer) handleRemoveResult(w http.ResponseWriter, r *http.Request) {
	req := &plugin.RemoveTaskReq{}
	unary(w, r, req, func() (proto.Message, error) {
		return &plugin.RemoveTaskResp{}, s.impl.RemoveResult(r.Context(), req.GetGid())
	})
}

func (s *downloaderServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	req := &plugin.DownloaderVersionReq{}
	unary(w, r, req, func() (proto.Message, error) {
		version, err := s.impl.Version(r.Context())
		if err != nil {
			return nil, err
		}
		return &plugin.DownloaderVersionResp{Version: proto.String(version)}, nil
	})
}
