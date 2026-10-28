package types

// Logger 驱动层的最小日志出口
//
// 为什么不用宿主的 pkg/llog：那份实现依赖宿主的 config / globals，只在宿主
// 进程里 Init 过；插件是独立进程，永远走不到 Init，L() 返回的是 io.Discard——
// 驱动的日志会被静默吞掉。而把 llog 链接进插件又等于把宿主的配置与全局状态
// 拖进一个与它无关的进程。
//
// 所以驱动层只声明这个最小接口，由运行环境接线：
//   - 内置驱动（宿主进程）：pkg/llog 在 Init 时接到宿主日志；
//   - 插件进程：plugin/sdk/logx 接到 stderr（宿主会逐行转发到宿主日志）。
//
// 未接线时是空实现，绝不 panic。
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// nopLogger 未接线时的空实现
type nopLogger struct{}

func (nopLogger) Debugf(string, ...any) {}
func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Warnf(string, ...any)  {}
func (nopLogger) Errorf(string, ...any) {}

var driverLogger Logger = nopLogger{}

// SetLogger 接线驱动层日志出口（传 nil 复位为空实现）
func SetLogger(l Logger) {
	if l == nil {
		driverLogger = nopLogger{}
		return
	}
	driverLogger = l
}

// Log 返回当前日志出口（永远可用）
func Log() Logger { return driverLogger }
