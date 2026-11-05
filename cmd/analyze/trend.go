//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tw93/mole/internal/units"
)

const (
	trendStoreFile        = "disk_trend_snapshots.json"
	maxTrendHistoryLength = 60
)

// TrendSnapshot captures directory sizes at a specific point in time.
type TrendSnapshot struct {
	Timestamp time.Time        `json:"timestamp"`
	Target    string           `json:"target"`
	Entries   map[string]int64 `json:"entries"`
}

// TrendHistory stores a sequence of snapshots for trend comparison.
type TrendHistory struct {
	Snapshots []TrendSnapshot `json:"snapshots"`
}

// TrendComparisonItem represents the difference for a single directory.
type TrendComparisonItem struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	CurrentSize int64   `json:"current_size"`
	PrevSize    int64   `json:"prev_size"`
	Delta       int64   `json:"delta"`
	GrowthPct   float64 `json:"growth_pct"`
	IsNew       bool    `json:"is_new"`
	Category    string  `json:"category"`
	Reason      string  `json:"reason"`
	Suggestion  string  `json:"suggestion"`
}

// TrendReport is the overall result of the trend analysis.
type TrendReport struct {
	Target       string                `json:"target"`
	IsOverview   bool                  `json:"is_overview"`
	CurrentTime  time.Time             `json:"current_time"`
	BaseTime     *time.Time            `json:"base_time,omitempty"`
	DaysAgo      float64               `json:"days_ago,omitempty"`
	IsInitial    bool                  `json:"is_initial"`
	TotalCurrent int64                 `json:"total_current"`
	TotalDelta   int64                 `json:"total_delta"`
	Items        []TrendComparisonItem `json:"items"`
}

func getTrendStorePath() (string, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, trendStoreFile), nil
}

func loadTrendHistory(storePath string) (*TrendHistory, error) {
	data, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &TrendHistory{Snapshots: []TrendSnapshot{}}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return &TrendHistory{Snapshots: []TrendSnapshot{}}, nil
	}
	var history TrendHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return &TrendHistory{Snapshots: []TrendSnapshot{}}, nil
	}
	return &history, nil
}

func saveTrendSnapshot(storePath string, snapshot TrendSnapshot) error {
	history, _ := loadTrendHistory(storePath)
	if history == nil {
		history = &TrendHistory{}
	}

	history.Snapshots = append(history.Snapshots, snapshot)
	if len(history.Snapshots) > maxTrendHistoryLength {
		history.Snapshots = history.Snapshots[len(history.Snapshots)-maxTrendHistoryLength:]
	}

	data, err := json.Marshal(history)
	if err != nil {
		return err
	}

	tmpFile := storePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, storePath)
}

func findBestBaselineSnapshot(history *TrendHistory, target string, targetDays int, now time.Time) *TrendSnapshot {
	if history == nil || len(history.Snapshots) == 0 {
		return nil
	}

	var best *TrendSnapshot
	targetDuration := time.Duration(targetDays) * 24 * time.Hour
	var minDiff time.Duration = -1

	for i := len(history.Snapshots) - 1; i >= 0; i-- {
		s := history.Snapshots[i]
		if s.Target != target {
			continue
		}
		age := now.Sub(s.Timestamp)
		if age < 5*time.Minute {
			// Skip snapshots taken just seconds/minutes ago in the same session
			continue
		}
		diff := age - targetDuration
		if diff < 0 {
			diff = -diff
		}
		if minDiff < 0 || diff < minDiff {
			minDiff = diff
			best = &history.Snapshots[i]
		}
	}

	// If no older snapshot found (except very recent ones), pick the oldest available if it's over 10m old
	if best == nil {
		for i := 0; i < len(history.Snapshots); i++ {
			s := history.Snapshots[i]
			if s.Target == target && now.Sub(s.Timestamp) >= 10*time.Minute {
				return &history.Snapshots[i]
			}
		}
	}

	return best
}

func analyzeDirectory(path string, delta int64) (category, reason, suggestion string) {
	lower := strings.ToLower(path)

	switch {
	case strings.Contains(lower, "deriveddata") || strings.Contains(lower, "developer/xcode"):
		category = "开发构建派生"
		reason = "Xcode 编译构建产物与索引，随着代码编写编译频繁增加，属典型高频再生缓存。"
		suggestion = "可安全清理，运行 `mo clean` 一键清理 Xcode 缓存与 DerivedData。"

	case strings.Contains(lower, "node_modules") || strings.Contains(lower, ".npm") ||
		strings.Contains(lower, ".cargo") || strings.Contains(lower, "pnpm") || strings.Contains(lower, "uv"):
		category = "依赖与包缓存"
		reason = "项目依赖库与包管理器本地存储，多项目拉取依赖时会快速扩张。"
		suggestion = "若项目仍在使用属正常资产；闲置时可执行对应的包管理器清理命令。"

	case strings.Contains(lower, "caches") || strings.Contains(lower, ".cache"):
		category = "运行时缓存"
		reason = "系统工具、浏览器或开发环境的临时缓存，正常使用中会自然累积。"
		suggestion = "属于可再生数据，运行 `mo clean` 审查并清理缓存。"

	case strings.Contains(lower, "downloads"):
		category = "用户下载目录"
		reason = "包含浏览器和应用下载的安装包、文档等，大增通常由主动下载引起。"
		suggestion = "可运行 `mo installer` 清理遗留 DMG/PKG 安装包，或用 `mo analyze ~/Downloads` 交互审查。"

	case strings.Contains(lower, "ollama") || strings.Contains(lower, "huggingface") || strings.Contains(lower, "models"):
		category = "AI 模型权重"
		reason = "本地大模型参数权重文件，单文件体积大，属于长期持久资产而非临时垃圾。"
		suggestion = "非系统垃圾，如不再需要请在对应客户端或目录中手动评估删除闲置模型。"

	case strings.Contains(lower, "mobilesync") || strings.Contains(lower, "backup"):
		category = "设备本地备份"
		reason = "iOS 设备本地全量备份，体积通常较大；新设备连接或系统更新前备份会引起激增。"
		suggestion = "若备份仍有恢复价值请保留；若属淘汰旧设备备份，可用 `mo analyze` 辅助删除。"

	case strings.Contains(lower, "containers") || strings.Contains(lower, "group containers"):
		category = "沙盒应用数据"
		reason = "macOS 沙盒应用（如微信、飞书等）的本地数据，聊天音视频与附件会导致体积膨胀。"
		suggestion = "建议在对应 App 内部进行聊天数据与缓存清理，或用 `mo analyze` 检查大文件。"

	default:
		if delta > 1<<30 {
			category = "空间显著增长"
			reason = "在指定周期内空间大幅增加，可能是临时日志暴涨、大文件写入或新数据导入。"
			suggestion = fmt.Sprintf("建议运行 `mo analyze %s` 下钻分析具体增长文件。", displayPath(path))
		} else {
			category = "常规数据目录"
			reason = "空间处于正常使用波动范围内。"
			suggestion = "无需特殊处理，可按需保持关注。"
		}
	}
	return category, reason, suggestion
}

func buildTrendReport(path string, isOverview bool, days int, scan jsonOutput) (TrendReport, error) {
	now := time.Now()
	storePath, err := getTrendStorePath()
	if err != nil {
		return TrendReport{}, err
	}

	currentEntries := make(map[string]int64, len(scan.Entries))
	for _, entry := range scan.Entries {
		if entry.Size > 0 {
			currentEntries[entry.Path] = entry.Size
		}
	}

	history, _ := loadTrendHistory(storePath)
	targetKey := path
	if isOverview {
		targetKey = "/"
	}

	baseline := findBestBaselineSnapshot(history, targetKey, days, now)

	// Save current snapshot
	_ = saveTrendSnapshot(storePath, TrendSnapshot{
		Timestamp: now,
		Target:    targetKey,
		Entries:   currentEntries,
	})

	report := TrendReport{
		Target:       targetKey,
		IsOverview:   isOverview,
		CurrentTime:  now,
		TotalCurrent: scan.TotalSize,
		Items:        make([]TrendComparisonItem, 0, len(scan.Entries)),
	}

	if baseline == nil {
		report.IsInitial = true
		for _, entry := range scan.Entries {
			if entry.Size <= 0 {
				continue
			}
			cat, rsn, sug := analyzeDirectory(entry.Path, 0)
			report.Items = append(report.Items, TrendComparisonItem{
				Name:        entry.Name,
				Path:        entry.Path,
				CurrentSize: entry.Size,
				Category:    cat,
				Reason:      rsn,
				Suggestion:  sug,
			})
		}
		slices.SortFunc(report.Items, func(a, b TrendComparisonItem) int {
			if a.CurrentSize > b.CurrentSize {
				return -1
			} else if a.CurrentSize < b.CurrentSize {
				return 1
			}
			return strings.Compare(a.Name, b.Name)
		})
		return report, nil
	}

	report.IsInitial = false
	report.BaseTime = &baseline.Timestamp
	report.DaysAgo = math.Round(now.Sub(baseline.Timestamp).Hours()/24*10) / 10

	var totalDelta int64
	for _, entry := range scan.Entries {
		if entry.Size <= 0 {
			continue
		}
		prev, found := baseline.Entries[entry.Path]
		delta := entry.Size - prev
		var growthPct float64
		if found && prev > 0 {
			growthPct = float64(delta) / float64(prev) * 100
		}
		cat, rsn, sug := analyzeDirectory(entry.Path, delta)

		report.Items = append(report.Items, TrendComparisonItem{
			Name:        entry.Name,
			Path:        entry.Path,
			CurrentSize: entry.Size,
			PrevSize:    prev,
			Delta:       delta,
			GrowthPct:   growthPct,
			IsNew:       !found,
			Category:    cat,
			Reason:      rsn,
			Suggestion:  sug,
		})
		totalDelta += delta
	}
	report.TotalDelta = totalDelta

	// Sort items: items with larger positive growth first, then by size
	slices.SortFunc(report.Items, func(a, b TrendComparisonItem) int {
		if a.Delta != b.Delta {
			if a.Delta > b.Delta {
				return -1
			}
			return 1
		}
		if a.CurrentSize > b.CurrentSize {
			return -1
		}
		return 1
	})

	return report, nil
}

func printTrendReport(report TrendReport) {
	fmt.Println()
	fmt.Println("📊 磁盘空间使用趋势与目录增长监测")
	fmt.Println(strings.Repeat("─", 65))

	if report.IsInitial {
		fmt.Println("💡 [初始快照已建立]")
		fmt.Printf("   已成功采集并记录当前 %d 个主要目录的空间大小基线。\n", len(report.Items))
		fmt.Println("   后续再次运行 `mo analyze --trend` 时，将自动计算增量变化与激增研判。")
		fmt.Println()
		fmt.Println("当前主要目录空间占用情况：")
		fmt.Printf("%-24s %12s   %s\n", "目录名称", "当前大小", "属性类型")
		fmt.Println(strings.Repeat("─", 65))
		for _, item := range report.Items {
			name := item.Name
			if displayWidth(name) > 22 {
				name = truncateMiddle(name, 22)
			}
			fmt.Printf("%-24s %12s   [%s]\n", name, units.BytesSI(item.CurrentSize), item.Category)
		}
		fmt.Println(strings.Repeat("─", 65))
		return
	}

	baseStr := report.BaseTime.Format("2006-01-02 15:04")
	fmt.Printf("📅 对比基准: %s (约 %.1f 天前)  vs  当前\n", baseStr, report.DaysAgo)
	deltaSign := "+"
	if report.TotalDelta < 0 {
		deltaSign = ""
	}
	fmt.Printf("📈 整体变化: %s%s (当前扫描总计: %s)\n",
		deltaSign, units.BytesSI(report.TotalDelta), units.BytesSI(report.TotalCurrent))
	fmt.Println()

	fmt.Printf("%-20s %10s %12s %8s   %s\n", "目录", "当前占用", "变化量 (Δ)", "增长率", "类型")
	fmt.Println(strings.Repeat("─", 65))

	var highlightItems []TrendComparisonItem

	for _, item := range report.Items {
		name := item.Name
		if displayWidth(name) > 18 {
			name = truncateMiddle(name, 18)
		}

		var deltaStr, growthStr string
		if item.IsNew {
			deltaStr = fmt.Sprintf("+%s (新)", units.BytesSI(item.CurrentSize))
			growthStr = "NEW"
		} else if item.Delta > 0 {
			deltaStr = fmt.Sprintf("+%s", units.BytesSI(item.Delta))
			growthStr = fmt.Sprintf("+%.1f%%", item.GrowthPct)
		} else if item.Delta < 0 {
			deltaStr = fmt.Sprintf("-%s", units.BytesSI(-item.Delta))
			growthStr = fmt.Sprintf("%.1f%%", item.GrowthPct)
		} else {
			deltaStr = "持平"
			growthStr = "0.0%"
		}

		fmt.Printf("%-20s %10s %12s %8s   %s\n",
			name,
			units.BytesSI(item.CurrentSize),
			deltaStr,
			growthStr,
			item.Category,
		)

		// Collect significant positive growth items (delta > 200MB or top positive growers)
		if item.Delta > 200<<20 || (item.Delta > 0 && len(highlightItems) < 3) {
			highlightItems = append(highlightItems, item)
		}
	}
	fmt.Println(strings.Repeat("─", 65))

	// Analysis and Recommendations section
	if len(highlightItems) > 0 {
		fmt.Println("\n🔍 重点激增目录深度分析与清理建议：")
		for i, item := range highlightItems {
			fmt.Printf("\n%d. %s (%s, 增加 +%s)\n",
				i+1, displayPath(item.Path), units.BytesSI(item.CurrentSize), units.BytesSI(item.Delta))
			fmt.Printf("   • 性质分析: %s\n", item.Reason)
			fmt.Printf("   • 处理建议: %s\n", item.Suggestion)
		}
	} else {
		fmt.Println("\n✨ 监测结果：未发现显著异常激增的目录，磁盘各区域增长平稳。")
	}
	fmt.Println()
}

func runTrendMode(path string, isOverview bool, days int, asJSON bool) {
	scan := performScanForJSON(path, isOverview)

	report, err := buildTrendReport(path, isOverview, days, scan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate trend report: %v\n", err)
		os.Exit(1)
	}

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode trend JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	printTrendReport(report)
}
