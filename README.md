# pfm-plugin-sdk

pfm 插件 SDK：**驱动插件与宿主程序之间唯一的契约**。

pfm 的存储驱动、离线下载后端都可以编译成独立进程（插件），宿主通过 loopback HTTP +
protojson 调用它们。这个仓库提供插件那一侧需要的全部东西：

- **ABI 数据定义**（proto）：驱动元数据、配置项、提供者视图、文件引用、枚举
- **线上服务定义**（proto）：`DriverService` / `DownloaderService` 的方法与消息
- **Go 值类型**：`driver/types` 里的 `StorageDriver` 接口、`FileRef`、`Provider`、配置解析……
- **插件运行时**：`plugin/sdk` 负责握手、Bearer 鉴权、RPC 分发、日志出口

插件只依赖本仓库和标准库，**不依赖宿主仓库的任何代码**——宿主怎么实现、内部表结构长什么样，
插件既不需要知道，也不允许知道。

## 为什么是独立模块

早先插件与宿主共处一个 Go module，插件 import 宿主的 `driver/types` 和 `proto/gen/native`。
代价很明显：

- 插件为了拿一个 `StorageDriver` 接口，被链接进 gorm、整张 ORM 表、宿主配置与全局状态；
- 宿主一改内部数据模型（ORM 字段、应用侧 proto），所有插件跟着编译失败，哪怕 ABI 没变；
- 无法给插件单独发版本，也没法让它对第三方作者可写。

现在**数据定义收归插件侧**（本仓库），宿主按 tag 依赖本仓库。宿主仍然保留自己的一份应用侧
proto（前端表单渲染、REST 响应、ORM 实体），那是宿主的事，与插件契约分开演进。

## 目录

| 路径 | 内容 |
|---|---|
| `proto/abi/` | 插件契约的数据定义：`driver.proto`（驱动元数据 / 配置项 / 文件引用）、`provider.proto`（提供者视图）、`enums.proto`（哈希、缓存状态、能力、配置项类型） |
| `proto/plugin/` | 线上服务：`driver.proto`（`DriverService`）、`downloader.proto`（`DownloaderService`） |
| `proto/gen/` | 由上面两棵子树生成的 Go 代码。**入库**（见「生成代码」） |
| `driver/types/` | 驱动要实现的值类型：`StorageDriver` 接口、`Provider`、`FileRef`、配置解析、上传分片辅助、加密流、文件名处理 |
| `pkg/crypt/` | 文件名加密（供驱动读写加密文件名使用） |
| `plugin/sdk/` | 插件进程运行时：`Serve` / `MustServe` |
| `plugin/sdk/logx/` | 插件日志（logrus，写 stderr，由宿主转发） |

## 最小插件

驱动包在 `init()` 里注册构造器与元数据，`main` 只负责启动：

```go
// driver/example/init.go
func init() {
    types.RegisterStorageDriverWithMetadata(newDriver, &abi.DriverInfo{
        DriverId:     proto.String("example"),
        Name:         proto.String("EXAMPLE"),
        DisplayName:  proto.String("示例网盘"),
        Capabilities: []abi.DriverCapability{abi.DriverCapability_DRIVER_CAPABILITY_BATCH_DELETE},
        ConfigFields: []*abi.ConfigField{
            {Name: proto.String("access_token"), FieldType: abi.ConfigFieldType_STRING.Enum(), Required: proto.Bool(true)},
        },
    })
}

// driver/example/driver.go
func newDriver(provider *types.Provider) (types.StorageDriver, error) { ... }

// main.go
func main() { sdk.MustServe("example") }
```

`types.Provider` 是宿主视角的提供者：`ID`、`DriverID`、`Name`、`ConfigJSON`（原样转发的配置
JSON，键名与 `ConfigField.Name` 一致）、`Config`（已解析好的配置项）、`Proxy`。驱动自己解析
配置，宿主管不着里面有什么。

配置项的值一律按字符串提交（前端表单就是这么发的），数字字段用 `sdk.FormNum` 兼容 `3` 与 `"3"`。

## 依赖约束（硬规则）

插件（含插件仓库里的驱动代码）**只许** import：

- 本仓库：`driver/types`、`driver/cryptostream`、`pkg/crypt`、`plugin/sdk`、`proto/gen/abi`、
  `proto/gen/plugin`
- 标准库与第三方公共库（logrus、protobuf……）

**禁止** import 宿主模块的任何包（`internal/*`、`proto/gen/native`、`proto/gen/entity`、
`gorm.io/gorm`、宿主日志/配置包）。插件是独立进程、独立发布物，一旦依赖宿主内部实现，
就等于把两个仓库重新绑死。

## ABI 版本

`abi/enums.proto` 里的 `ABI_VERSION_1` 与 `plugin/sdk.AbiVersion` 必须一致（当前都是 `1`）。
插件在握手与 `Describe` 响应里带上这个版本，宿主校验不一致就拒绝加载。

规则：

- **加字段 / 加枚举值**：不升版本，向后兼容（protojson 会忽略未知字段）。
- **删字段、改字段语义、改方法签名**：必须升 ABI 版本，并同步升级宿主与所有插件。
- 枚举的数值一旦发布就别复用；不用 `reserved`（项目约定：字段号连续，删除后不保留空号）。

## 生成代码

`proto/gen/` 是**生成后入库**的，消费方不需要装 buf / protoc 就能 `go build`。
代价是必须防止它与 `.proto` 漂移，所以 CI 会重新生成并 `git diff --exit-code`。

本地改完 `.proto`：

```bash
./dev.sh proto      # 等价于 (cd proto && buf generate)，需要 buf 与 protoc-gen-go
git add proto/gen
```

版本要求：buf 1.66+，protoc-gen-go 1.36+。

## 开发命令

```bash
./dev.sh check     # gofmt 检查 + vet + build + test（提交前跑）
./dev.sh proto     # 重新生成 proto/gen
./dev.sh test      # 跑测试
./dev.sh fmt       # gofmt -w .
./dev.sh tidy      # go mod tidy
```

## 版本策略

语义化版本。宿主、插件、本仓库三方共用同一份 ABI，任何一处升级都要一起验证：

- `v0.x` 期间允许破坏性变更，但每次破坏性变更必须**同时**升级宿主与插件，并在发布说明里写清改了哪条 ABI。
- 取依赖的方式两种都支持：按 tag 的 `require`，或用 `replace` 指向同级目录（本地联调）；
  选哪种都不改变上面的配套升级要求。

驱动实现（如百度网盘、阿里云盘驱动）不在本仓库，它们各自是一个独立仓库 / 模块，
只依赖本仓库。

## 许可

AGPL-3.0，见 [LICENSE](LICENSE)。
