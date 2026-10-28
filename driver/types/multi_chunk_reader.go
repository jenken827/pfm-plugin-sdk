package types

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/jenken827/pfm-plugin-sdk/pkg/crypt"
)

// ChunkOpener 分片打开器接口
// 不同驱动实现此接口来打开分片（可能使用 HTTP Range、本地文件读取等方式）
type ChunkOpener interface {
	// OpenChunk 打开指定分片
	// offset: 分片内偏移（支持从中间位置开始读取）
	// 返回的 ReadCloser 应该已经定位到 offset 位置
	OpenChunk(ctx context.Context, ref FileRef, offset int64) (io.ReadCloser, error)
	// OpenChunkRangeSeek 返回一个 OpenRangeSeek 函数，支持 Range 请求和 Seek 操作
	// 用于加密文件的解密
	OpenChunkRangeSeek(ctx context.Context, ref FileRef) OpenRangeSeek
}

// MultiChunkReader 多分片合并读取器
// 支持将多个分片合并为一个连续的 ReadSeekCloser。
//
// 长度与偏移一律工作在「明文空间」：加密分片在存储上是密文
// （FileRef.ChunkSize 记录的是密文长度），必须换算回明文长度再参与
// totalSize / chunkOffset 计算。否则 Seek(io.SeekEnd) 会返回密文总长，
// 上层（如 net/http.ServeContent）据此声明的 Content-Length 将大于
// reader 实际能输出的明文字节数，响应被判定为不完整、连接被截断
// （客户端表现为 "Empty reply from server"）。
//
// 解密侧同样以明文偏移定位（pkg/crypt 的 calculateUnderlying 把明文
// offset 换算为底层密文偏移），故 openCurrentChunk 传入的
// chunkInternalOffset 也依赖 chunkOffset 是明文累积值。
type MultiChunkReader struct {
	ctx         context.Context
	opener      ChunkOpener // 分片打开器
	locations   []FileRef
	encryptor   Encryptor
	currentIdx  int
	currentRC   io.ReadCloser
	offset      int64
	totalSize   int64
	chunkOffset []int64 // 每个分片的起始偏移（明文空间，预先计算）
	mu          sync.Mutex
}

// NewMultiChunkReader 创建多分片合并读取器
// 预先按明文空间计算总大小与每个分片的起始偏移。
// 加密分片长度非法（如数据损坏）时返回错误，由调用方提前失败，
// 避免在响应头写出后才暴露问题导致连接截断。
func NewMultiChunkReader(ctx context.Context, opener ChunkOpener, locations []FileRef, encryptor Encryptor) (*MultiChunkReader, error) {
	encrypted := encryptor != nil && encryptor.IsEncrypted()

	// 预先计算明文总大小和每个分片的起始偏移
	var totalSize int64
	chunkOffset := make([]int64, len(locations))
	for i, loc := range locations {
		plainSize, err := plainChunkSize(int64(loc.ChunkSize), encrypted)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: %w", loc.ChunkNumber, err)
		}
		chunkOffset[i] = totalSize
		totalSize += plainSize
	}

	return &MultiChunkReader{
		ctx:         ctx,
		opener:      opener,
		locations:   locations,
		encryptor:   encryptor,
		currentIdx:  0,
		offset:      0,
		totalSize:   totalSize,
		chunkOffset: chunkOffset,
	}, nil
}

// plainChunkSize 把存储分片大小换算为明文大小
// 未加密分片的存储大小即明文大小；加密分片存储的是密文
// （33B 文件头 + 每 64KiB 明文块 16B 认证标签），用 DecryptedSize 精确换算。
func plainChunkSize(storedSize int64, encrypted bool) (int64, error) {
	if !encrypted {
		return storedSize, nil
	}
	return crypt.DecryptedSize(storedSize)
}

// Read 读取数据
func (m *MultiChunkReader) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果当前分片已读完，切换到下一个
	for m.currentIdx < len(m.locations) {
		// 如果当前 Reader 未打开，打开它
		if m.currentRC == nil {
			rc, err := m.openCurrentChunk()
			if err != nil {
				return 0, err
			}
			m.currentRC = rc
		}

		// 尝试从当前 Reader 读取
		n, err = m.currentRC.Read(p)
		if err == io.EOF {
			// 当前分片读完，关闭并切换到下一个
			m.currentRC.Close()
			m.currentRC = nil
			m.currentIdx++
			if n > 0 {
				m.offset += int64(n)
				return n, nil
			}
			continue
		}

		m.offset += int64(n)
		return n, err
	}

	return 0, io.EOF
}

// Seek 定位
func (m *MultiChunkReader) Seek(offset int64, whence int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = m.offset + offset
	case io.SeekEnd:
		newOffset = m.totalSize + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if newOffset < 0 {
		return 0, fmt.Errorf("negative position: %d", newOffset)
	}

	if newOffset > m.totalSize {
		return 0, fmt.Errorf("position beyond end: %d > %d", newOffset, m.totalSize)
	}

	// 查找目标分片（二分查找优化）
	targetIdx := m.findChunkIndex(newOffset)

	// 位置变化时关闭当前分片，下次 Read 按新偏移重新打开。
	// 必须无条件重置：同分片内的 Seek 若不重置，底层 lazy reader
	// （如加密 decrypter）的内部缓冲不会回退，导致预读/Seek 后
	// 输出丢字节（如 preReadStream 的 Read(1)+Seek(-1) 会丢首字节，
	// 造成实际输出比 Content-Length 少，Go 截断连接）。
	if m.currentRC != nil {
		m.currentRC.Close()
		m.currentRC = nil
	}

	m.currentIdx = targetIdx
	m.offset = newOffset

	return newOffset, nil
}

// Close 关闭
func (m *MultiChunkReader) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentRC != nil {
		return m.currentRC.Close()
	}
	return nil
}

// openCurrentChunk 打开当前分片
func (m *MultiChunkReader) openCurrentChunk() (io.ReadCloser, error) {
	ref := m.locations[m.currentIdx]

	// 计算分片内偏移
	chunkInternalOffset := m.offset - m.chunkOffset[m.currentIdx]

	// 如果需要解密
	if m.encryptor != nil && m.encryptor.IsEncrypted() {
		// 使用 DecryptDataSeek 处理加密文件
		// 这会先读取 header，然后 seek 到正确的位置
		openRangeSeek := m.opener.OpenChunkRangeSeek(m.ctx, ref)
		decrypted, err := m.encryptor.DecryptDataSeek(m.ctx, openRangeSeek, chunkInternalOffset, -1)
		if err != nil {
			return nil, fmt.Errorf("decrypt chunk failed: %w", err)
		}
		return decrypted, nil
	}

	// 非加密文件，直接打开
	rc, err := m.opener.OpenChunk(m.ctx, ref, chunkInternalOffset)
	if err != nil {
		return nil, err
	}

	return rc, nil
}

// findChunkIndex 查找偏移量对应的分片索引（二分查找）
func (m *MultiChunkReader) findChunkIndex(offset int64) int {
	// 简单线性查找（对于小规模分片足够快）
	// 如果分片数量很大，可以改用二分查找
	for i := len(m.chunkOffset) - 1; i >= 0; i-- {
		if offset >= m.chunkOffset[i] {
			return i
		}
	}
	return 0
}

// TotalSize 返回明文总大小
func (m *MultiChunkReader) TotalSize() int64 {
	return m.totalSize
}
