# 多机安全清理与 MBX 集成记录

本文档记录针对 MacBook Pro 本机及两台远程 Mac Mini 的安全清理、MBX 构建缓存收录与集群自动化联动方案。

## 1. 设备与环境架构

| 设备 | 角色 / 架构 | Mole 安装路径 | 关键存储与挂载点 |
| :--- | :--- | :--- | :--- |
| **MacBook Pro** | M1 Max (主控机) | `/Users/jason/Dev/tools/disk-clean` | 内置 2TB SSD (APFS) |
| **Jason Mac Mini** | Apple Silicon | `/Users/jason/Dev/tools/disk-clean` | 内置 256GB 系统盘 (`/`) |
| **Nicolas Mac Mini**| Apple Silicon | `/Volumes/UltraDisk/Dev2/tools/disk-clean` | 系统盘 (`/`) + 扩展 2TB 盘 (`/Volumes/UltraDisk`) |

---

## 2. MBX 构建缓存收录

### 2.1 变更内容
- **集成位置**：`lib/clean/dev.sh` 中的 `clean_dev_rust`
- **功能逻辑**：检测到 `mbx` 且存在 `~/Library/Caches/mbx` 时，使用 `clean_tool_cache` 自动触发 `mbx gc`，安全回收陈旧构建与废弃 target 目录。
- **单测覆盖**：在 `tests/clean_dev_caches.bats` 中增加 `clean_dev_mbx` 单元测试。

### 2.2 本机清理收益
- 执行 `mbx gc` 立即回收了 1.1GB 废弃目标与陈旧 checkout。
- 执行 `./mole clean` 释放 **34.73 GB** 无用应用与系统缓存。

---

## 3. 远程 Mac Mini 部署与清理表现

### 3.1 Jason Mac Mini
- **部署**：通过 Tailscale SSH 免密克隆至 `~/Dev/tools/disk-clean`。
- **清理结果**：系统盘可用空间从原本告急的 **19 GB** 激增至 **31 GB**（净增 **+12 GB**），大幅缓解了系统盘压力。

### 3.2 Nicolas Mac Mini
- **部署**：克隆至扩展盘 `/Volumes/UltraDisk/Dev2/tools/disk-clean`。
- **系统盘清理**：清理 137 项缓存，释放 6.45GB，系统盘可用容量提升至 17.86GB。
- **2TB 扩展盘 (`/Volumes/UltraDisk`)**：
  - 调用 Mole 原生 `--external /Volumes/UltraDisk` 进行安全扫描。
  - 白名单自动保护 `.pnpm-store`、`mbx` 缓存及工程文件，未删动任何未知文件与备份，盘面健康可用空间约 1.4TB。
- **大文件安全保留上报**：
  - Hugging Face cache: 55.26GB
  - OrbStack data: 37.20GB
  - 符合安全守则，均未擅自触碰。

---

## 4. 集群自动化维护脚本

脚本路径：[`tools/sh/mo-cluster-clean.sh`](file:///Users/jason/Dev/tools/sh/mo-cluster-clean.sh)

### 4.1 核心特性
1. **保守安全模式**：强制以 `MOLE_TEST_NO_AUTH=1 < /dev/null` 非交互运行，遇 sudo 提权自动跳过，不阻断、不卡死。
2. **全设备覆盖**：按序调度 MacBook Pro、Jason Mac Mini 以及 Nicolas Mac Mini（含内置系统盘与 2T 扩展盘）。
3. **Rust 缓存回收**：各机器在完成通用清理后自动触发 `mbx gc` 进行垃圾回收。

### 4.2 运行方式
```bash
/Users/jason/Dev/tools/sh/mo-cluster-clean.sh
```
