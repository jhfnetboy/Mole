# Mole 与 MBX (Mr. Boxington) 安装与使用指南

本文档介绍 macOS 终端清理优化工具 **Mole** 与 Rust 跨工作区共享构建缓存工具 **MBX** 的安装与使用方法。

---

## 1. Mole - macOS 系统清理与磁盘分析工具

- **官方仓库**：[tw93/Mole](https://github.com/tw93/Mole)
- **开发分支仓库**：[jhfnetboy/Mole](https://github.com/jhfnetboy/Mole)

### 1.1 安装方式

**方式一：官方一键安装（推荐）**
```bash
curl -fsSL https://raw.githubusercontent.com/tw93/mole/main/install.sh | bash
```

**方式二：本地源码编译安装**
```bash
git clone https://github.com/jhfnetboy/Mole.git
cd Mole
make build
# 运行本地构建二进制：
./mole
```

### 1.2 常用命令

| 命令 | 说明 |
|---|---|
| `mo` | 启动交互式终端主菜单 |
| `mo clean` | 安全清理系统缓存、日志与开发工具垃圾（支持 `--dry-run` 预览） |
| `mo uninstall <App>` | 彻底卸载应用及其关联残留配置与缓存 |
| `mo analyze [目录]` | 启动基于 BubbleTea 的图形化磁盘占用分析 TUI |
| `mo analyze --trend` | **时序空间变化监测**：对比 7 天前后目录大小增量 $\Delta$，分析激增合理性并给出清理建议 |
| `mo analyze --trend --days 14` | 指定对比历史时间窗口（如 14 天） |
| `mo status` | 紧凑型系统状态与硬件健康度看板 |

---

## 2. MBX (Mr. Boxington) - Rust 跨工作区共享编译缓存

- **作者**：jdx (mise 作者)
- **源码与文档**：[github.com/jdx/mr-boxington](https://github.com/jdx/mr-boxington)

### 2.1 安装方式

**方式一：通过 Cargo 安装（推荐）**
```bash
cargo install mbx --locked
```

**方式二：通过 mise 安装**
```bash
mise use --global --tool-option mr_boxington=true rust mr-boxington
```

### 2.2 全局初始化与环境配置

1. 运行初始化设置：
   ```bash
   mbx setup
   ```
2. 将 Cargo Shim 代理路径加入系统环境变量（写入 `~/.zshrc` 或 `~/.bashrc`）：
   ```bash
   export PATH="$HOME/Library/Application Support/mbx/bin:$PATH"
   ```
3. 重新加载配置后，所有 `cargo` 命令将自动经由 `mbx` 拦截加速。

### 2.3 常用操作指令

- **旧仓库迁移接入**：进入已有 Rust 项目根目录执行：
  ```bash
  mbx adopt
  ```
  *(无损直接接管现存 `target/` 目录，将其纳入全局共享池，省去全量重编)*
- **正常构建**：各项目无感正常使用：
  ```bash
  cargo build
  cargo test
  cargo clippy
  ```
- **查看缓存节省与命中统计**：
  ```bash
  mbx stats
  ```
- **健康度与配置检查**：
  ```bash
  mbx doctor
  ```
- **手动触发磁盘缓存回收 (GC)**：
  ```bash
  mbx gc
  ```
