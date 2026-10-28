package types

import "context"

// RemoteEntry 远端对象条目（供 cleanup-storage-orphans 等离线工具枚举）。
//
// Path 必须与 file_locations.path 采用**同一路径形态**（同一根、前导斜杠一致），
// 这样 CLI 才能直接用路径与数据库记录比对出「逆向孤儿」。
type RemoteEntry struct {
	Path  string // 完整路径（与 file_locations.path 同形态）
	Name  string // 文件名
	ID    string // 提供者侧文件 ID（删物理副本时填进 FileRef.ProviderFileID）
	IsDir bool   // 是否目录
	Size  int64  // 文件字节数（目录为 0）
}

// RemoteLister 可选能力：枚举提供者上的远端对象（一次一层，由调用方递归）。
//
// 只对「按目录树寻址」的提供者有意义（local / 网盘类）。像 Telegram 这种
// 只能按消息 ID 读取、没有目录概念的驱动不实现该接口，CLI 会提示不支持。
type RemoteLister interface {
	// RemoteRoot 返回 pfm 数据的根路径（默认扫描起点），与 file_locations.path 同形态
	RemoteRoot() string
	// ListRemote 列出 dir 下的直接子项；dir 为 RemoteRoot() 或其子目录
	ListRemote(ctx context.Context, dir string) ([]RemoteEntry, error)
}
