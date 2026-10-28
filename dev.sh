#!/usr/bin/env bash
# pfm-plugin-sdk 开发脚本：生成 proto 代码、构建、测试、格式化
#
# 与宿主仓库 pfm 的 dev.sh 保持同一套子命令风格，但这里没有 Flutter / Web /
# 原生库那些东西——SDK 只有 Go 代码和 proto。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

# buf / protoc-gen-go 一般装在 ~/go/bin（go install 的默认位置）
export PATH="${PATH}:${HOME}/go/bin"

usage() {
  cat <<'EOF'
用法: ./dev.sh <命令>

命令:
  proto     生成 Go protobuf 代码（proto/gen/，入库）
  build     编译所有包
  test      运行所有测试
  vet       go vet
  fmt       gofmt 并修正格式（-l 只列出不合规文件）
  tidy      go mod tidy
  check     fmt 检查 + vet + build + test（CI 用同一套）
  clean     清理构建产物（不碰 proto/gen）
  help      显示此帮助

示例:
  ./dev.sh proto                    # 改完 .proto 后重新生成
  ./dev.sh check                    # 提交前自检
  ./dev.sh test -run TestFormNum    # 传参给 go test
EOF
}

cmd_proto() {
  command -v buf >/dev/null || { echo "缺少 buf：go install github.com/bufbuild/buf/cmd/buf@latest" >&2; exit 1; }
  command -v protoc-gen-go >/dev/null || { echo "缺少 protoc-gen-go：go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" >&2; exit 1; }
  echo "📦 生成 Go protobuf 代码..."
  ( cd proto && buf generate )
  echo "✅ 生成完成（proto/gen/ 记得一起提交）"
}

cmd_fmt() {
  if [[ "${1:-}" == "-l" ]]; then
    local out
    out="$(gofmt -l .)"
    if [[ -n "$out" ]]; then
      echo "以下文件格式不合规：" >&2
      echo "$out" >&2
      return 1
    fi
    return 0
  fi
  gofmt -w .
  echo "✅ 格式化完成"
}

cmd_check() {
  echo "🔍 格式检查..."
  cmd_fmt -l
  echo "🔍 go vet..."
  go vet ./...
  echo "🔨 编译..."
  go build ./...
  echo "🧪 测试..."
  go test ./...
  echo "✅ 全部通过"
}

case "${1:-help}" in
  proto) shift; cmd_proto "$@" ;;
  build) shift; go build ./... ;;
  test)  shift; go test "$@" ./... ;;
  vet)   shift; go vet ./... ;;
  fmt)   shift; cmd_fmt "$@" ;;
  tidy)  shift; go mod tidy ;;
  check) shift; cmd_check ;;
  clean) go clean -cache -testcache 2>/dev/null || true; rm -f ./pfm-plugin-sdk ;;
  help|-h|--help) usage ;;
  *) echo "未知命令: $1" >&2; usage; exit 1 ;;
esac
