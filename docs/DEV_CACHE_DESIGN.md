# 开发缓存协议与 Mole 个人开发版

本分支把“先清理、再重新下载和构建”改为“先登记并复用，再审核不必要的缓存”。
仓库声明它使用的资源，由全局工具提供缓存位置和运行配置；Mole 读取同一份
注册信息，保护这些资源。本文区分第一阶段接口与后续路线，不代表所有设计已经实现。

## 约定与边界

- 已登记的模型、共享包缓存和构建缓存默认保留。仓库消失或长期未使用只构成审核理由。
- 未登记不等于可删除。目录名、文件年龄和“可以重新下载”都不是单独的删除授权。
- 临时产物可不逐个登记，但未来清理必须受明确路径范围、内容和使用状态检查约束。
- 本机工作范围是 `~/Dev`、用户批准的缓存与模型目录，以及明确的系统缓存路径。
  其他目录不得深入扫描或清理；必要时只获得目录整体大小。
- 注册、分配和配置复用不授权清理，也不授权自动安装工具链、迁移数据或改写 shell 配置。
- 第一阶段不改变通用 `mo clean` 的扫描范围。不要把它当作本协议的范围限定清理入口。

## 三个执行部分

| 部分 | 职责 |
|---|---|
| 全局 `dev-cache` skill | 识别仓库需求，调用 CLI 登记、获取配置并验证接入 |
| `bin/dev-cache` 与 Go CLI | 维护注册表，分配目录，生成环境，运行和检查工具 |
| 本分支的 Mole | 在已有清理和删除路径中检查登记保护，拒绝删除受保护资源 |

Skill 是工作入口，CLI 执行确定性规则。仅安装全局 skill 不能强制每个 agent
或终端遵守约定。接入仓库还要在 `AGENTS.md` 中记录入口，并把普通构建脚本接到
`dev-cache run`；`doctor` 用于发现实际缓存位置与登记位置不一致。

## 资源分类

| 分类 | 例子 | 生命周期 |
|---|---|---|
| 长期资源 | 模型权重、明确安装的工具 | 默认保留，未来按名称明确审核移除 |
| 可复用缓存 | pnpm store、uv wheel 缓存、Go 构建缓存 | 默认保留，未来通过所属工具做有边界的整理 |
| 临时产物 | 一次性测试、打包和实验输出 | 未来在明确清理范围内审核，不能按未注册自动删除 |

包下载缓存与全局安装包分开，Python 虚拟环境与 uv 缓存分开，模型权重与推理缓存
分开。共享一份下载或可复用编译结果，不代表所有项目可以共享一份可变构建目录。

## 第一阶段的目录与身份

```text
~/.local/state/dev-cache/
  registry.json            # 资源登记；原子替换与并发写入保护

~/.local/share/dev-cache/
  ...                      # CLI 分配的共享或隔离资源目录

<Mole fork>/skills/dev-cache/
  SKILL.md                 # 全局 skill 的版本化来源
  scripts/dev-cache        # 与调用者当前目录无关的执行入口
```

`DEV_CACHE_STATE_DIR` 和 `DEV_CACHE_DATA_DIR` 分别覆盖状态和数据根目录，主要用于
隔离测试或明确配置。第一阶段用版本化 JSON 注册表，不引入数据库依赖；策略文件、
仓库清单格式和更复杂的查询有实际需求后再扩展。

仓库身份使用 Git common directory，worktree 使用自身的物理 checkout 身份。
同一仓库的多个 worktree 可以登记相同的共享包缓存；Rust 可变构建输出按 worktree
隔离。第一阶段不自动搬迁旧目录，`--path` 可把已有资源原地纳入保护。

## 第一阶段命令

从本分支根目录执行 `make build` 后使用 `./bin/dev-cache`。以下接口用于建立
“登记 → 获取配置 → 验证 → 构建使用 → Mole 保护”的最小闭环：

```bash
./bin/dev-cache register --repo /absolute/path/to/repository --kind uv-cache
./bin/dev-cache register --repo /absolute/path/to/repository --kind go-build
./bin/dev-cache register --repo /absolute/path/to/repository --kind go-mod
./bin/dev-cache list --repo /absolute/path/to/repository
./bin/dev-cache env --repo /absolute/path/to/repository
./bin/dev-cache doctor --repo /absolute/path/to/repository
./bin/dev-cache run --repo /absolute/path/to/repository -- go build ./...
```

`env` 输出 JSON，供人或程序检查，不经 shell `eval` 执行。`run` 将登记得到的配置
传给指定命令；它本身不批准运行该命令产生的副作用。
`--repo` 选择登记信息，不改变调用者的当前工作目录。上述构建示例应从目标仓库
目录执行；如从其他目录运行，应使用工具自身支持的目录参数。

| `--kind` | 接入方式 | 第一阶段边界 |
|---|---|---|
| `pnpm-store` | 共享 pnpm 包缓存位置 | 不合并不同项目的 `node_modules` |
| `uv-cache` | `UV_CACHE_DIR` | 不合并虚拟环境，不直接改写 uv 缓存内部文件 |
| `go-build` | `GOCACHE` | 由 Go 自己判断缓存命中 |
| `go-mod` | `GOMODCACHE` | 模块缓存与最终可执行文件分开 |
| `rust-target` | `CARGO_TARGET_DIR` | 每个 checkout/worktree 隔离；尚不实现跨仓库编译复用 |
| `model` | 登记已有模型目录 | 只登记和保护，不下载、迁移、去重或删除模型 |

登记已有模型的示例：

```bash
./bin/dev-cache register --repo /absolute/path/to/repository --kind model \
  --path /absolute/path/to/existing/models --name speech-models
```

重复登记应返回已有资源，不能悄悄更换其位置。`doctor` 只使用有界的只读探测；
工具不可用或无法确认位置时必须如实报告，不能通过自动下载工具使检查变绿。

## 全局 skill 安装与仓库接入

```bash
./scripts/install-dev-cache-skill.sh
```

默认在 `${CODEX_HOME:-$HOME/.codex}/skills/dev-cache` 建立指向本分支
`skills/dev-cache` 的链接。安装可重复执行；已有无关文件或 skill 时拒绝覆盖。
它不修改 shell profile，也不把可执行文件写到全局 `PATH`。移动本仓库后需要修复链接。
如需撤销，只移除安装脚本建立的那个链接，不删除源 skill、注册表或缓存数据。

其他仓库可以在其 `AGENTS.md` 约定：

> 为本仓库配置或运行开发构建前，使用全局 `dev-cache` skill 查询已有登记。
> 复用其返回的缓存配置；新增模型或共享缓存先登记。未登记目录不能直接判为可删除。

安装、登记、运行构建和接入其他仓库是不同动作，不因一项完成而自动授权其余动作。

## Mole 的保护契约

`guard --path <absolute-path>` 是清理端的确定性接口：退出码 `0` 表示没有与已登记
资源重叠，`3` 表示受保护，`2` 表示状态错误。`0` 只表示本注册表不阻止此路径，
不表示这个路径可删除，Mole 原有的内容、权限、运行状态和授权检查仍须通过。

保护范围包括登记路径、其子路径，以及会连带删除资源的父目录。比较必须按路径
组件进行，不能把同名前缀的兄弟目录误当作子目录；词法路径与实际符号链接目标都要检查。
存在但损坏、不可读的注册表，或缺少需要的 guard 二进制，均不得使清理放行。

直接删除、Trash、dry-run、purge，以及包管理器等所属工具的清理调用，都应遵守
同一保护。保护目录的同时调用会清空该目录的命令不构成保护。实现边界与剩余未接入
入口必须在验证记录中如实说明，不能用提示词替代执行层检查。

## 验收与递进路线

第一阶段在临时 fixture 中验收，不清理用户的真实缓存：

1. 重复登记返回同一资源，多仓库共享受支持的包缓存。
2. 不同 Rust worktree 获得不同可变构建目录。
3. `run` 向子命令传递登记配置，`doctor` 能区分匹配、冲突与工具不可用。
4. Mole 保留受保护路径、子路径和父目录，同时仍可处理无关且原本合法的 fixture。
5. 损坏状态拒绝清理；并发登记不丢记录；符号链接不能绕过保护。
6. 全局 skill 从其他工作目录也能调用此分支中的 CLI。

后续按验证结果逐步叠加：

- **范围与清单：** 全局策略文件、仓库 `.dev-cache.toml`、严格扫描/清理根目录约束。
- **真实复用：** 完成更多工具版本适配，评估 Rust sccache 和 TS 任务缓存；用真实构建验证命中，不能只比较路径。
- **使用关系：** 模型版本、共享 blob、仓库引用、运行租约及清理时的并发协议。
- **审核与迁移：** 旧目录发现、可恢复迁移、空间预算与所属工具的选择性整理。

以上后续能力尚不是第一阶段承诺。模型或仓库记录过期不触发自动删除；不会为了
减少磁盘数字而反复清空需要重新下载、重新构建的资源。
