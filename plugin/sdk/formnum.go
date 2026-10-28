package sdk

import (
	"encoding/json"
	"strconv"
	"strings"
)

// FormNum 表单里的数值字段（ConfigFieldType_NUMBER）
//
// 同一个配置项在不同客户端/不同版本里的 JSON 写法并不统一：元数据是 NUMBER，
// 有的客户端写 3，有的写 "3"。配置解析失败会让整个 provider 构造不出来，所以
// 统一用这个类型接：两种写法都接受，序列化时写回字符串（驱动内部只当文本读，
// 需要整数时用 Int()）。
type FormNum string

// UnmarshalJSON 接受 3 / "3" / null / ""
func (f *FormNum) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "" || s == "null":
		*f = ""
	case strings.HasPrefix(s, `"`):
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*f = FormNum(strings.TrimSpace(str))
	default:
		*f = FormNum(s)
	}
	return nil
}

// String 原始文本
func (f FormNum) String() string { return string(f) }

// Int 解析为整数；空值或非法值返回 0（0 的语义由调用方决定）
func (f FormNum) Int() int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(string(f)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
