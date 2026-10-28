package types

import (
	"context"
	"fmt"
	"path"
	"testing"
	"time"
)

// mockDirBackend 内存目录后端，用于测试 AllocateUploadDir
type mockDirBackend struct {
	// dirEntries[dir] = 该目录下的条目
	dirEntries map[string][]DirEntry
}

func (m *mockDirBackend) List(ctx context.Context, dir string) ([]DirEntry, error) {
	return m.dirEntries[dir], nil
}

func (m *mockDirBackend) Mkdir(ctx context.Context, dir string) error {
	if _, ok := m.dirEntries[dir]; !ok {
		m.dirEntries[dir] = nil
	}
	return nil
}

func yearMonthDir(root string) string {
	now := time.Now()
	return path.Join(root, now.Format("2006"), now.Format("01"))
}

// TestAllocateUploadDir_Empty 空目录直接使用年月目录
func TestAllocateUploadDir_Empty(t *testing.T) {
	m := &mockDirBackend{dirEntries: map[string][]DirEntry{}}
	dir, err := AllocateUploadDir(context.Background(), "/root", m)
	if err != nil {
		t.Fatal(err)
	}
	if dir != yearMonthDir("/root") {
		t.Errorf("expected %s, got %s", yearMonthDir("/root"), dir)
	}
}

// TestAllocateUploadDir_NotFull 文件数未满直接使用年月目录
func TestAllocateUploadDir_NotFull(t *testing.T) {
	ym := yearMonthDir("/root")
	files := make([]DirEntry, MaxFilesPerDir-1)
	for i := range files {
		files[i] = DirEntry{Name: fmt.Sprintf("f%d", i), IsDir: false}
	}
	m := &mockDirBackend{dirEntries: map[string][]DirEntry{ym: files}}
	dir, err := AllocateUploadDir(context.Background(), "/root", m)
	if err != nil {
		t.Fatal(err)
	}
	if dir != ym {
		t.Errorf("expected %s, got %s", ym, dir)
	}
}

// TestAllocateUploadDir_Full 文件满时创建数字子目录 1
func TestAllocateUploadDir_Full(t *testing.T) {
	ym := yearMonthDir("/root")
	files := make([]DirEntry, MaxFilesPerDir)
	for i := range files {
		files[i] = DirEntry{Name: fmt.Sprintf("f%d", i), IsDir: false}
	}
	m := &mockDirBackend{dirEntries: map[string][]DirEntry{ym: files}}
	dir, err := AllocateUploadDir(context.Background(), "/root", m)
	if err != nil {
		t.Fatal(err)
	}
	if dir != path.Join(ym, "1") {
		t.Errorf("expected %s, got %s", path.Join(ym, "1"), dir)
	}
}

// TestAllocateUploadDir_SubDirIncrement 数字子目录满时递增
func TestAllocateUploadDir_SubDirIncrement(t *testing.T) {
	ym := yearMonthDir("/root")
	sub1 := path.Join(ym, "1")
	// 年月目录下有一个数字子目录 "1"（已满）
	sub1Files := make([]DirEntry, MaxFilesPerDir)
	for i := range sub1Files {
		sub1Files[i] = DirEntry{Name: fmt.Sprintf("s%d", i), IsDir: false}
	}
	m := &mockDirBackend{dirEntries: map[string][]DirEntry{
		ym:   {{Name: "1", IsDir: true}},
		sub1: sub1Files,
	}}
	dir, err := AllocateUploadDir(context.Background(), "/root", m)
	if err != nil {
		t.Fatal(err)
	}
	if dir != path.Join(ym, "2") {
		t.Errorf("expected %s, got %s", path.Join(ym, "2"), dir)
	}
}
