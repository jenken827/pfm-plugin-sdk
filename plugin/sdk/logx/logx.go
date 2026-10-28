// Package logx 插件进程内的日志出口
//
// 插件与宿主是两个进程：宿主只接管插件的 stdout/stderr，并把每一行转发进宿主
// 日志（前缀 `[plugin <driver> stderr]`）。所以插件写 stderr 就是最直接的通道。
//
// 为什么不用宿主的 pkg/llog：它依赖宿主的 config 与 globals，只在宿主进程里
// Init 过；插件里永远不会走到 Init，L() 返回的是 io.Discard —— 驱动的日志会被
// 静默吞掉（历史上就是这样丢的）。而把 llog 链接进插件，等于把宿主的配置与
// 全局状态拖进一个跟它无关的进程。
//
// 用法与 llog 保持一致，方便驱动代码无痛切换：
//
//	logx.L().Infof("upload started, size=%d", size)
//	logx.C(ctx).Warnf("retry: %v", err)
package logx

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	// EnvLevel 日志级别环境变量（debug/info/warn/error），默认 info
	EnvLevel = "PFM_PLUGIN_LOG_LEVEL"
)

var (
	mu     sync.RWMutex
	logger *logrus.Logger
	// driverID 驱动身份，作为每条日志的固定字段（宿主转发时也会带，双保险便于检索）
	driverID string
)

// Init 初始化插件日志。id 为驱动身份，可重复调用（幂等）。
func Init(id string) {
	mu.Lock()
	defer mu.Unlock()
	driverID = id
	if logger != nil {
		return
	}
	l := logrus.New()
	l.SetOutput(os.Stderr)
	l.SetFormatter(&logrus.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true, // 宿主转发时会加上自己的时间戳
		DisableQuote:     true,
	})
	l.SetLevel(parseLevel(os.Getenv(EnvLevel)))
	logger = l
}

// L 日志入口（带 driver 字段）
func L() *logrus.Entry {
	return base().WithField("driver", id())
}

// C 与宿主 llog.C(ctx) 用法兼容；插件侧没有宿主的 request_id，忽略 ctx
func C(context.Context) *logrus.Entry {
	return L()
}

// SetLevel 运行时调整级别（debug/info/warn/error，非法值按 info）
func SetLevel(level string) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l != nil {
		l.SetLevel(parseLevel(level))
	}
}

func base() *logrus.Logger {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l != nil {
		return l
	}
	// 未 Init（例如驱动单测里直接调日志）：兜底到 discard，绝不 panic
	fallback := logrus.New()
	fallback.SetOutput(os.Stderr)
	fallback.SetLevel(logrus.InfoLevel)
	return fallback
}

func id() string {
	mu.RLock()
	defer mu.RUnlock()
	return driverID
}

func parseLevel(s string) logrus.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return logrus.DebugLevel
	case "warn", "warning":
		return logrus.WarnLevel
	case "error":
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
	}
}
