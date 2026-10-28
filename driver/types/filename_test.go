package types

import (
	"strings"
	"testing"
)

// TestGetUniqueFileNameFormat 验证唯一文件名的格式：保留扩展名且插入随机串+时间戳
func TestGetUniqueFileNameFormat(t *testing.T) {
	name := GetUniqueFileName("photo.jpg")

	if name == "photo.jpg" {
		t.Fatal("file name should always be renamed")
	}
	if !strings.HasPrefix(name, "photo_") {
		t.Fatalf("expected photo_ prefix, got %s", name)
	}
	if !strings.HasSuffix(name, ".jpg") {
		t.Fatalf("expected .jpg suffix preserved, got %s", name)
	}

	// 格式: photo_<8位随机>_<14位时间戳>.jpg
	core := strings.TrimSuffix(strings.TrimPrefix(name, "photo_"), ".jpg")
	parts := strings.Split(core, "_")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (random_timestamp), got %v", parts)
	}
	if len(parts[0]) != 8 {
		t.Fatalf("expected 8-char random part, got %q", parts[0])
	}
	if len(parts[1]) != 14 {
		t.Fatalf("expected 14-digit timestamp part, got %q", parts[1])
	}
}

// TestGetUniqueFileNameNoExtension 无扩展名文件也能正确重命名
func TestGetUniqueFileNameNoExtension(t *testing.T) {
	name := GetUniqueFileName("README")
	if name == "README" {
		t.Fatal("file name should always be renamed")
	}
	if !strings.HasPrefix(name, "README_") {
		t.Fatalf("expected README_ prefix, got %s", name)
	}
}

// TestGetUniqueFileNameUnique 并发调用不产生重名
func TestGetUniqueFileNameUnique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		name := GetUniqueFileName("a.txt")
		if seen[name] {
			t.Fatalf("duplicate file name generated: %s", name)
		}
		seen[name] = true
	}
}
