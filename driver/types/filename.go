package types

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"
)

// DefaultEncryptionSuffix 默认加密文件后缀
const DefaultEncryptionSuffix = ".bin"

// randomHexString 生成 n 字节随机数的十六进制字符串（长度为 2n）
func randomHexString(n int) (string, error) {
	randBytes := make([]byte, n)
	if _, err := rand.Read(randBytes); err != nil {
		Log().Errorf("generate random bytes failed: %v", err)
		return "", fmt.Errorf("generate random bytes failed: %w", err)
	}
	return hex.EncodeToString(randBytes), nil
}

// GenerateObfuscatedFilename 生成混淆文件名
// originalName: 原始文件名（仅用于日志记录）
// suffix: 加密后缀（可选，如 ".bin"，如果为空则使用默认值）
//
// 返回格式: 随机字符串_时间戳.后缀
// 示例: a1b2c3d4_20260404142345.bin
func GenerateObfuscatedFilename(originalName, suffix string) (string, error) {
	// 如果后缀为空，使用默认值
	if suffix == "" {
		suffix = DefaultEncryptionSuffix
	}
	// 确保后缀以点开头
	if len(suffix) > 0 && suffix[0] != '.' {
		suffix = "." + suffix
	}

	// 生成8位随机字符串
	randomStr, err := randomHexString(4)
	if err != nil {
		return "", err
	}

	// 添加时间戳
	timestamp := time.Now().Format("20060102150405")

	// 最终文件名：随机字符串_时间戳.后缀
	obfuscatedName := fmt.Sprintf("%s_%s%s", randomStr, timestamp, suffix)
	Log().Infof("obfuscated filename: %s -> %s", originalName, obfuscatedName)

	return obfuscatedName, nil
}

// GetUniqueFileName 获取唯一上传文件名
// 无论文件是否加密，驱动上传到存储提供者时都必须重命名：上传目录按 年/月
// 动态分配（见 AllocateUploadDir），来自不同逻辑目录的同名文件会落入同一
// 物理目录，沿用原名会被后上传者覆盖（本地存储 os.Create 截断、百度同路径
// 覆盖）。随机字符串 + 时间戳保证并发/跨时间都不重名，同时保留原扩展名
// （明文文件仍需按扩展名识别文件类型）。
//
// 返回格式: 原文件名_随机字符串_时间戳.扩展名
// 示例: photo_a1b2c3d4_20260404142345.jpg
func GetUniqueFileName(fileName string) string {
	timestamp := time.Now().Format("20060102150405")

	// 分离文件名和扩展名
	ext := filepath.Ext(fileName)
	name := fileName[:len(fileName)-len(ext)]

	if randomStr, err := randomHexString(4); err == nil {
		return fmt.Sprintf("%s_%s_%s%s", name, randomStr, timestamp, ext)
	}
	// 随机数生成失败时退化为纯时间戳命名（同一秒内并发同名概率极低）
	return fmt.Sprintf("%s_%s%s", name, timestamp, ext)
}
