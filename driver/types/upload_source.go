package types

import (
	"github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"
)

// FileRef 文件在存储提供者上的引用（值类型）
// 双向使用：
//   - Upload 返回值：驱动填充 ProviderFileID/Name/Path/Size/Hash 等，LocationID 为空
//   - Open 输入：由宿主经 driver/types/orm.FileRefFromLocation 从 FileLocationORM 转换，
//     LocationID 指向 DB 记录
//
// 替代 StorageProviderFileInfo 与直接传 FileLocationORM，驱动不再依赖 GORM ORM 类型。
type FileRef struct {
	LocationID     uint64          // FileLocation 主键（DB 定位，用于缓存状态更新等；Upload 返回时为空）
	ProviderFileID string          // provider 侧文件 ID（baidu fs_id / telegram file_id / local 文件名）
	Name           string          // 文件名（Upload 返回时填充，Open 时可为空）
	Path           string          // 存储路径
	Size           uint64          // 存储大小（加密后）
	ChunkSize      uint64          // 分片大小
	Hash           string          // provider 侧 hash（MD5/etag 等）
	ChunkHash      string          // 分片 hash
	ChunkNumber    uint32          // 分片序号（从 0 开始）
	ChannelID      string          // telegram 专用
	MessageID      int             // telegram 专用
	CacheStatus    abi.CacheStatus // telegram 专用（缓存状态）
}

// UploadSource 上传源（替代 UploadRecordORM）
type UploadSource struct {
	TempPath string // 明文源文件本地路径
	FileName string // 原始文件名
	FileSize uint64 // 原始文件大小（字节）
	FileHash string // blake3 哈希（仅日志/追踪用）
}

// UploadOptions 上传选项
type UploadOptions struct {
	Encryptor        Encryptor // per-provider 加密器，nil 或 NullEncryptor 表示不加密
	ObfuscateName    bool      // 是否混淆文件名（加密时通常为 true）
	EncryptionSuffix string    // 加密后缀（如 ".bin"）
}

// OpenOptions 打开选项
type OpenOptions struct {
	Encryptor Encryptor // per-provider 解密器，nil 或 NullEncryptor 表示不加密
}

// ProviderConfig 存储提供者的类型化配置视图
// 替代 GetOrm() 暴露整个 ORM，业务层只读需要的字段
type ProviderConfig struct {
	EncryptionKey        string
	EncryptionSuffix     string
	SortOrder            uint32
	FileSizeLimit        uint64
	MaxConcurrentUploads uint32
	HashType             abi.HashType
	Enabled              bool
}
