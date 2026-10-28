package types

import (
	"context"
	"io"

	"github.com/jenken827/pfm-plugin-sdk/pkg/crypt"
)

// OpenRangeSeek 打开文件的函数类型
type OpenRangeSeek = crypt.OpenRangeSeek

// Encryptor 加密器接口
type Encryptor interface {
	// Encrypt 加密数据流
	Encrypt(in io.Reader) (io.ReadCloser, error)
	// Decrypt 解密数据流
	Decrypt(in io.ReadCloser) (io.ReadCloser, error)
	// DecryptDataSeek 解密数据流（支持 Seek 操作）
	// 用于加密文件的分片下载
	DecryptDataSeek(ctx context.Context, open OpenRangeSeek, offset, limit int64) (ReadSeekCloser, error)
	// IsEncrypted 是否加密
	IsEncrypted() bool
}

// EncryptorParamProvider 可选接口：能给出重建加密器所需的原始参数
//
// 跨进程（插件）场景下宿主不能传递 Encryptor 对象，只能把 key+salt 下发，
// 由插件侧用 driver/cryptostream 重建等价加密器。
type EncryptorParamProvider interface {
	EncryptorParams() (key, salt string)
}

// EncryptorParams 取加密器的重建参数；不加密或实现不了时返回 ok=false
func EncryptorParams(enc Encryptor) (key, salt string, ok bool) {
	if enc == nil || !enc.IsEncrypted() {
		return "", "", false
	}
	provider, ok := enc.(EncryptorParamProvider)
	if !ok {
		return "", "", false
	}
	key, salt = provider.EncryptorParams()
	return key, salt, key != ""
}

// NullEncryptor 空加密器（不加密）
type NullEncryptor struct{}

// Encrypt 加密
func (n *NullEncryptor) Encrypt(in io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(in), nil
}

// Decrypt 解密
func (n *NullEncryptor) Decrypt(in io.ReadCloser) (io.ReadCloser, error) {
	return in, nil
}

// DecryptDataSeek 解密数据流（支持 Seek 操作）
func (n *NullEncryptor) DecryptDataSeek(ctx context.Context, open OpenRangeSeek, offset, limit int64) (ReadSeekCloser, error) {
	rc, err := open(ctx, offset, limit)
	if err != nil {
		return nil, err
	}
	return &nullReadSeekCloser{rc}, nil
}

// IsEncrypted 是否启用加密
func (n *NullEncryptor) IsEncrypted() bool {
	return false
}

// nullReadSeekCloser 包装 io.ReadCloser 为 ReadSeekCloser
type nullReadSeekCloser struct {
	io.ReadCloser
}

// Seek 实现 io.Seeker 接口
func (n *nullReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := n.ReadCloser.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, nil
}
