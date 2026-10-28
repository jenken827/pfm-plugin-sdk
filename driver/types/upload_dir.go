package types

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"time"
)

// MaxFilesPerDir 每个目录最大文件数
// 当年月目录下的文件数达到此值时，创建数字子文件夹
const MaxFilesPerDir = 500

// DirEntry 目录条目
type DirEntry struct {
	Name  string
	IsDir bool
}

// DirBackend 目录后端（List/Mkdir 由各驱动适配：local 用 os，云端用 API）
type DirBackend interface {
	// List 列出目录下所有条目
	List(ctx context.Context, dir string) ([]DirEntry, error)
	// Mkdir 创建目录（应容忍"已存在"）
	Mkdir(ctx context.Context, dir string) error
}

// AllocateUploadDir 年/月/数字子目录 sharding 的单一实现
// 当年月目录文件数达 MaxFilesPerDir 时创建/递增数字子目录
// 路径统一使用正斜杠；local backend 由 os 自动转换
func AllocateUploadDir(ctx context.Context, root string, backend DirBackend) (string, error) {
	now := time.Now()
	year := now.Format("2006")
	month := now.Format("01")

	// 逐级确保目录存在（百度 create 不递归创建父目录，必须逐级；失败仅告警不阻断，
	// 由 List fallback 与后续 create 兜底——恢复重构前的容错行为）
	for _, dir := range []string{root, path.Join(root, year), path.Join(root, year, month)} {
		if err := backend.Mkdir(ctx, dir); err != nil {
			Log().Warnf("ensure directory %s failed (may already exist): %v", dir, err)
		}
	}

	yearMonthDir := path.Join(root, year, month)

	entries, err := backend.List(ctx, yearMonthDir)
	if err != nil {
		// 列出失败（如百度 -7 目录不存在/名错误）→ 直接使用年月目录，后续 create 兜底
		Log().Warnf("list year-month directory failed: %v, fallback to %s", err, yearMonthDir)
		return yearMonthDir, nil
	}

	directFileCount := 0
	maxSubDirNum := 0
	for _, entry := range entries {
		if entry.IsDir {
			if n, err := strconv.Atoi(entry.Name); err == nil && n > maxSubDirNum {
				maxSubDirNum = n
			}
		} else {
			directFileCount++
		}
	}

	// 情况1：没有数字子目录，且直接文件数未满 → 直接用年月目录
	if maxSubDirNum == 0 && directFileCount < MaxFilesPerDir {
		return yearMonthDir, nil
	}
	// 情况2：没有数字子目录但文件已满 → 创建 1/
	if maxSubDirNum == 0 {
		subDir := path.Join(yearMonthDir, "1")
		if err := backend.Mkdir(ctx, subDir); err != nil {
			return "", fmt.Errorf("create subdirectory 1 failed: %w", err)
		}
		return subDir, nil
	}
	// 情况3：有数字子目录 → 检查最大子目录的文件数
	maxSubDir := path.Join(yearMonthDir, strconv.Itoa(maxSubDirNum))
	subEntries, err := backend.List(ctx, maxSubDir)
	if err != nil {
		// 子目录列表失败（如百度 -7）→ 创建新子目录（恢复重构前行为）
		Log().Warnf("list subdirectory %d failed: %v, creating new subfolder", maxSubDirNum, err)
		newSubDir := path.Join(yearMonthDir, strconv.Itoa(maxSubDirNum+1))
		if err := backend.Mkdir(ctx, newSubDir); err != nil {
			return "", fmt.Errorf("create subdirectory %d failed: %w", maxSubDirNum+1, err)
		}
		return newSubDir, nil
	}
	subFileCount := 0
	for _, entry := range subEntries {
		if !entry.IsDir {
			subFileCount++
		}
	}
	if subFileCount < MaxFilesPerDir {
		return maxSubDir, nil
	}
	newSubDir := path.Join(yearMonthDir, strconv.Itoa(maxSubDirNum+1))
	if err := backend.Mkdir(ctx, newSubDir); err != nil {
		return "", fmt.Errorf("create subdirectory %d failed: %w", maxSubDirNum+1, err)
	}
	return newSubDir, nil
}
