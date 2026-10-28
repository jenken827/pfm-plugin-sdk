package types

import "github.com/jenken827/pfm-plugin-sdk/proto/gen/abi"

// HasCapability 判断驱动元数据是否声明了某项能力。
//
// 宿主据此决定行为（是否可直链、是否支持目录枚举、是否需要预下载…），不再按
// 具体驱动身份分支：驱动声明什么能力，宿主就做什么事。
func HasCapability(md *abi.DriverInfo, capability abi.DriverCapability) bool {
	if md == nil {
		return false
	}
	for _, c := range md.GetCapabilities() {
		if c == capability {
			return true
		}
	}
	return false
}
