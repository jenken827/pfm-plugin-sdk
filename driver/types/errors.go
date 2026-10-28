package types

import "errors"

// ErrPermanent 标记"重试也不会改变结果"的失败（远端硬限制、参数错误等）。
//
// 驱动可以把底层错误包一层：`fmt.Errorf("...: %w", types.ErrPermanent)`，宿主用
// IsPermanent 判断即可——不需要认识具体驱动，也不需要匹配错误文案。
var ErrPermanent = errors.New("permanent failure")

// IsPermanent 判断错误链里是否带了 ErrPermanent。
func IsPermanent(err error) bool {
	return err != nil && errors.Is(err, ErrPermanent)
}
