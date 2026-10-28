package types

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/jenken827/pfm-plugin-sdk/pkg/crypt"
)

// fakeChunkOpener 用内存字节模拟分片存储，按「存储（密文）偏移」提供读取。
// 与真实驱动语义一致：offset 之前的数据丢弃，读到分片末尾。
type fakeChunkOpener struct {
	chunks [][]byte // 下标 = 分片序号
}

func (f *fakeChunkOpener) OpenChunk(_ context.Context, ref FileRef, offset int64) (io.ReadCloser, error) {
	idx := int(ref.ChunkNumber)
	if idx < 0 || idx >= len(f.chunks) {
		return nil, fmt.Errorf("chunk %d out of range", idx)
	}
	data := f.chunks[idx]
	if offset < 0 || offset > int64(len(data)) {
		return nil, fmt.Errorf("offset %d out of range for chunk %d (len=%d)", offset, idx, len(data))
	}
	return io.NopCloser(bytes.NewReader(data[offset:])), nil
}

func (f *fakeChunkOpener) OpenChunkRangeSeek(_ context.Context, ref FileRef) OpenRangeSeek {
	return func(ctx context.Context, offset, _ int64) (io.ReadCloser, error) {
		return f.OpenChunk(ctx, ref, offset)
	}
}

// fakeEncryptor 用真实 crypt.Cipher 提供 Encryptor 实现（加密分片场景）
type fakeEncryptor struct {
	cipher *crypt.Cipher
}

func (e *fakeEncryptor) Encrypt(in io.Reader) (io.ReadCloser, error) {
	return e.cipher.EncryptData(in)
}

func (e *fakeEncryptor) Decrypt(in io.ReadCloser) (io.ReadCloser, error) {
	return e.cipher.DecryptData(in)
}

func (e *fakeEncryptor) DecryptDataSeek(ctx context.Context, open OpenRangeSeek, offset, limit int64) (ReadSeekCloser, error) {
	return e.cipher.DecryptDataSeek(ctx, open, offset, limit)
}

func (e *fakeEncryptor) IsEncrypted() bool { return true }

// encryptChunk 用给定 cipher 独立加密一个分片，返回密文字节
func encryptChunk(t *testing.T, c *crypt.Cipher, plain []byte) []byte {
	t.Helper()
	rc, err := c.EncryptData(bytes.NewReader(plain))
	if err != nil {
		t.Fatalf("encrypt chunk: %v", err)
	}
	defer rc.Close()
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	return out
}

func newTestCipher(t *testing.T) *crypt.Cipher {
	t.Helper()
	c, err := crypt.NewCipher("test-password", "test-salt")
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return c
}

// TestMultiChunkReaderEncryptedTotalSizeIsPlaintext 回归：加密分片的总长度必须是明文长度。
//
// 旧实现用密文长度（含 33B 文件头 + 每 64KiB 块 16B 认证标签）累加 totalSize，
// 使 Seek(io.SeekEnd) 返回密文总长；上层 net/http.ServeContent 据此声明的
// Content-Length 大于 reader 实际能输出的明文字节数 → 响应被判为不完整、
// 连接被截断（WebDAV GET 加密文件时报 "Empty reply from server"）。
func TestMultiChunkReaderEncryptedTotalSizeIsPlaintext(t *testing.T) {
	cipher := newTestCipher(t)
	plain := []byte("0123456789") // 与线上 get-test.json 同规模（10B 明文 / 59B 密文）

	ciphertext := encryptChunk(t, cipher, plain)
	if len(ciphertext) <= len(plain) {
		t.Fatalf("precondition failed: ciphertext (%d) must exceed plaintext (%d)", len(ciphertext), len(plain))
	}

	opener := &fakeChunkOpener{chunks: [][]byte{ciphertext}}
	refs := []FileRef{{ChunkNumber: 0, ChunkSize: uint64(len(ciphertext))}}

	r, err := NewMultiChunkReader(context.Background(), opener, refs, &fakeEncryptor{cipher: cipher})
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	defer r.Close()

	if got := r.TotalSize(); got != int64(len(plain)) {
		t.Errorf("TotalSize: expected plaintext length %d, got %d", len(plain), got)
	}
	pos, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("seek end: %v", err)
	}
	if pos != int64(len(plain)) {
		t.Errorf("Seek(SeekEnd): expected %d, got %d", len(plain), pos)
	}

	// 模拟 ServeContent：Seek(End) 定长 → Seek(Start) → 全量输出
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek start: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if !bytes.Equal(data, plain) {
		t.Errorf("content mismatch: expected %q, got %q", plain, data)
	}
}

// TestMultiChunkReaderEncryptedMultiChunk 加密多分片（跨 64KiB 块边界）：
// 明文偏移定位、跨分片拼接、分片内 Seek 必须全部正确。
func TestMultiChunkReaderEncryptedMultiChunk(t *testing.T) {
	cipher := newTestCipher(t)

	part1 := bytes.Repeat([]byte("A"), 70*1024) // 跨块
	part2 := bytes.Repeat([]byte("B"), 33*1024)
	plain := append(append([]byte{}, part1...), part2...)

	c1 := encryptChunk(t, cipher, part1)
	c2 := encryptChunk(t, cipher, part2)
	opener := &fakeChunkOpener{chunks: [][]byte{c1, c2}}
	refs := []FileRef{
		{ChunkNumber: 0, ChunkSize: uint64(len(c1))},
		{ChunkNumber: 1, ChunkSize: uint64(len(c2))},
	}

	r, err := NewMultiChunkReader(context.Background(), opener, refs, &fakeEncryptor{cipher: cipher})
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	defer r.Close()

	if got := r.TotalSize(); got != int64(len(plain)) {
		t.Fatalf("TotalSize: expected %d, got %d", len(plain), got)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if !bytes.Equal(data, plain) {
		t.Fatalf("full read mismatch: got %d bytes, expected %d", len(data), len(plain))
	}

	// 跨分片边界 Seek：定位到第二片中部，剩余内容必须精确匹配
	offset := int64(len(part1) + 1024)
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		t.Fatalf("seek %d: %v", offset, err)
	}
	tail, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read tail: %v", err)
	}
	if !bytes.Equal(tail, part2[1024:]) {
		t.Fatalf("tail mismatch: got %d bytes, expected %d", len(tail), len(part2)-1024)
	}

	// 分片内 Seek（第一片内部）
	inner := int64(65*1024 + 7)
	if _, err := r.Seek(inner, io.SeekStart); err != nil {
		t.Fatalf("seek %d: %v", inner, err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("read at %d: %v", inner, err)
	}
	if !bytes.Equal(buf, plain[inner:inner+4]) {
		t.Errorf("inner seek mismatch: got %q, expected %q", buf, plain[inner:inner+4])
	}
}

// TestMultiChunkReaderPlainChunks 未加密分片：chunkSize 即明文大小，不减开销
func TestMultiChunkReaderPlainChunks(t *testing.T) {
	chunks := [][]byte{[]byte("hello "), []byte("world")}
	want := "hello world"

	opener := &fakeChunkOpener{chunks: chunks}
	refs := []FileRef{
		{ChunkNumber: 0, ChunkSize: uint64(len(chunks[0]))},
		{ChunkNumber: 1, ChunkSize: uint64(len(chunks[1]))},
	}

	r, err := NewMultiChunkReader(context.Background(), opener, refs, &NullEncryptor{})
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	defer r.Close()

	if got := r.TotalSize(); got != int64(len(want)) {
		t.Errorf("TotalSize: expected %d, got %d", len(want), got)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if string(data) != want {
		t.Errorf("content: expected %q, got %q", want, data)
	}
}

// TestMultiChunkReaderRejectsCorruptEncryptedChunk 加密分片长度非法时构造即失败，
// 避免在响应头写出后才暴露（截断连接）。
func TestMultiChunkReaderRejectsCorruptEncryptedChunk(t *testing.T) {
	refs := []FileRef{{ChunkNumber: 0, ChunkSize: 4}} // 小于文件头长度
	_, err := NewMultiChunkReader(context.Background(), &fakeChunkOpener{chunks: [][]byte{{1, 2, 3, 4}}}, refs, &fakeEncryptor{cipher: newTestCipher(t)})
	if err == nil {
		t.Fatal("expected error for corrupt encrypted chunk, got nil")
	}
}
