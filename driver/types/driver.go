package types

import (
	"context"
	"io"
	"time"
)

// ReadSeekCloser 支持定位的 Reader（用于断点续传）
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// StorageDriver 存储驱动
type StorageDriver interface {
	// GetType 返回驱动身份（稳定字符串，如 local / baidu，见 ids.go）
	GetType() string
	GetName() string
	// GetConfig 获取驱动自身配置
	GetConfig() any
	// GetProviderConfig 获取存储提供者的类型化配置视图
	GetProviderConfig() *ProviderConfig
	// Upload 上传文件
	// src: 上传源（明文本地文件）；opt: 上传选项（含 per-provider 加密器）
	// 返回文件在提供者上的引用列表（分片场景为多个）
	Upload(ctx context.Context, src UploadSource, opt UploadOptions) ([]FileRef, error)
	// Open 打开文件（支持多分片合并、断点续传）
	// refs: 该文件在当前存储提供者的所有分片引用（已按 chunk_number 排序）
	// opt: 打开选项（含解密器）
	Open(ctx context.Context, refs []FileRef, opt OpenOptions) (ReadSeekCloser, error)
}

// Closer 关闭接口（可选）
type Closer interface {
	Close()
}

// DeleteResult 删除结果
type DeleteResult struct {
	ID      uint64 // FileLocationORM.Id
	Success bool   // 是否成功
	Error   error  // 失败时的错误信息
	// Permanent 表示这次失败重复尝试也不会成功（平台硬限制、参数错误等）。
	// 驱动最清楚自己的远端语义，由它给出这个判断；宿主只看标志位决定记 warning
	// 还是 error，不按驱动身份或错误文案猜。
	Permanent bool
}

// BatchDeleter 批量删除接口
type BatchDeleter interface {
	// BatchDelete 批量删除文件
	// 返回值说明：
	// - results=nil, err=nil: 全部执行成功
	// - results=nil, err!=nil: 全部执行失败
	// - results!=nil: 部分成功，需检查每条记录的状态
	BatchDelete(ctx context.Context, refs []FileRef) (results []DeleteResult, err error)
}

// PreDownloader 预下载接口（可选）
// 用于支持大文件预缓存的存储驱动（如 Telegram）
type PreDownloader interface {
	// PreDownload 预下载文件的所有分片
	// refs: 该文件在当前存储提供者的所有分片引用
	// 返回第一个错误，如果没有错误返回 nil
	PreDownload(ctx context.Context, refs []FileRef) error
}

// DirectLink 存储提供者签发的下载直链（客户端可绕开服务端直连）
//
// URL 自带时效与鉴权，等价于一份临时凭证：不要落库、不要写进日志。
type DirectLink struct {
	URL string
	// Headers 客户端必须原样携带的请求头（如百度/天翼要求的 User-Agent）；
	// 为空表示 URL 自包含鉴权、任何客户端都能直接用。
	Headers map[string]string
	// ExpiresAt 链接过期时间；零值表示驱动无法给出（按"尽快使用"处理）
	ExpiresAt time.Time
	// SupportsRange 驱动声明的 Range 支持情况——仅供参考，真实能力由
	// `pfm direct-link --probe` 实测为准（部分提供者会忽略 Range 返回 200 全量）。
	SupportsRange bool
}

// DirectLinker 直链能力（可选实现）
//
// 只对**单个物理对象**成立：调用方必须先确认该 provider 上只有一条完整位置行，
// 且内容未加密（密文需要服务端持密钥解密），多分片无法用一个 URL 表达拼接。
type DirectLinker interface {
	DirectLink(ctx context.Context, ref FileRef) (*DirectLink, error)
}
