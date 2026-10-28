package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestComputeEncryptedChunkSize 验证加密开销预留
func TestComputeEncryptedChunkSize(t *testing.T) {
	limit := int64(1024 * 1024 * 1024)

	// 不加密：返回原值
	if got := ComputeEncryptedChunkSize(limit, false); got != limit {
		t.Errorf("non-encrypted: expected %d, got %d", limit, got)
	}

	// 加密：应小于原值（预留开销）
	if got := ComputeEncryptedChunkSize(limit, true); got >= limit {
		t.Errorf("encrypted: expected < %d, got %d", limit, got)
	}
}

// TestGenerateObfuscatedFilename 验证混淆文件名
func TestGenerateObfuscatedFilename(t *testing.T) {
	name, err := GenerateObfuscatedFilename("测试文件.txt", ".bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".bin") {
		t.Errorf("expected .bin suffix, got %s", name)
	}
	if strings.Contains(name, "测试文件") {
		t.Errorf("original name should not appear in obfuscated name: %s", name)
	}

	// 空后缀使用默认值
	name2, err := GenerateObfuscatedFilename("a.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name2, DefaultEncryptionSuffix) {
		t.Errorf("expected default suffix %s, got %s", DefaultEncryptionSuffix, name2)
	}
}

// TestPackToTempFilePlainRename 验证未加密上传同样必须重命名（防同目录同名覆盖）
func TestPackToTempFilePlainRename(t *testing.T) {
	src := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, name, size, err := PackToTempFile(src, "hello.txt", UploadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if path != src {
		t.Errorf("plain upload should reuse source path, got %s", path)
	}
	if name == "hello.txt" {
		t.Error("plain upload file name must be renamed (not only encrypted)")
	}
	if !strings.HasSuffix(name, ".txt") {
		t.Errorf("plain upload should preserve original extension, got %s", name)
	}
	if size != 2 {
		t.Errorf("expected size 2, got %d", size)
	}

	// 两次调用结果必须不同（随机串保证唯一，避免并发同名覆盖）
	_, name2, _, err := PackToTempFile(src, "hello.txt", UploadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if name == name2 {
		t.Errorf("two plain uploads should produce different names, both %s", name)
	}
}

// TestPackToTempFileObfuscateRename 验证混淆名分支仍走完全替换
func TestPackToTempFileObfuscateRename(t *testing.T) {
	src := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, name, _, err := PackToTempFile(src, "secret.txt", UploadOptions{
		ObfuscateName:    true,
		EncryptionSuffix: ".enc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(name, "secret") {
		t.Errorf("obfuscated name must not contain original name, got %s", name)
	}
	if !strings.HasSuffix(name, ".enc") {
		t.Errorf("expected .enc suffix, got %s", name)
	}
}
