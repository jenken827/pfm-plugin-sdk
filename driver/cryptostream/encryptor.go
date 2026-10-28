package cryptostream

import (
	"context"
	"io"

	"github.com/jenken827/pfm-plugin-sdk/driver/types"
	"github.com/jenken827/pfm-plugin-sdk/pkg/crypt"
)

// CryptEncryptor 基于 crypt.Cipher 的加密器
type CryptEncryptor struct {
	cipher *crypt.Cipher
	// 保留原始参数：插件模式下宿主需要把 key+salt 下发给插件进程，
	// 让插件用公共包重建同一个加密器（cipher 本身不可序列化）
	password string
	salt     string
}

// NewCryptEncryptor 创建加密器
func NewCryptEncryptor(password, salt string) (*CryptEncryptor, error) {
	cipher, err := crypt.NewCipher(password, salt)
	if err != nil {
		return nil, err
	}
	return &CryptEncryptor{cipher: cipher, password: password, salt: salt}, nil
}

// EncryptorParams 实现 types.EncryptorParamProvider：给出重建参数
func (e *CryptEncryptor) EncryptorParams() (string, string) {
	return e.password, e.salt
}

// Encrypt 加密数据
func (e *CryptEncryptor) Encrypt(in io.Reader) (io.ReadCloser, error) {
	return e.cipher.EncryptData(in)
}

// Decrypt 解密数据
func (e *CryptEncryptor) Decrypt(in io.ReadCloser) (io.ReadCloser, error) {
	return e.cipher.DecryptData(in)
}

// DecryptDataSeek 解密数据流（支持 Seek 操作）
func (e *CryptEncryptor) DecryptDataSeek(ctx context.Context, open types.OpenRangeSeek, offset, limit int64) (types.ReadSeekCloser, error) {
	return e.cipher.DecryptDataSeek(ctx, open, offset, limit)
}

// IsEncrypted 是否加密
func (e *CryptEncryptor) IsEncrypted() bool {
	return true
}

// GetEncryptor 根据存储提供者和文件夹信息获取加密器
// encryptionKey: 存储提供者的加密密钥
// fileSalt: 文件夹的加密盐值
func GetEncryptor(encryptionKey string, fileSalt []byte) (types.Encryptor, error) {
	if encryptionKey == "" || len(fileSalt) == 0 {
		// 不加密
		return &types.NullEncryptor{}, nil
	}

	// 创建加密器
	return NewCryptEncryptor(encryptionKey, string(fileSalt))
}
