//go:build darwin

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrendSnapshotSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	storeFile := filepath.Join(tempDir, trendStoreFile)

	history, err := loadTrendHistory(storeFile)
	if err != nil {
		t.Fatalf("unexpected error loading empty trend history: %v", err)
	}
	if len(history.Snapshots) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(history.Snapshots))
	}

	snap1 := TrendSnapshot{
		Timestamp: time.Now().Add(-7 * 24 * time.Hour),
		Target:    "/",
		Entries: map[string]int64{
			"/Applications": 20 << 30,
			"/Library":      10 << 30,
		},
	}
	if err := saveTrendSnapshot(storeFile, snap1); err != nil {
		t.Fatalf("failed to save snapshot 1: %v", err)
	}

	snap2 := TrendSnapshot{
		Timestamp: time.Now(),
		Target:    "/",
		Entries: map[string]int64{
			"/Applications": 25 << 30,
			"/Library":      10 << 30,
		},
	}
	if err := saveTrendSnapshot(storeFile, snap2); err != nil {
		t.Fatalf("failed to save snapshot 2: %v", err)
	}

	loaded, err := loadTrendHistory(storeFile)
	if err != nil {
		t.Fatalf("failed to load saved trend history: %v", err)
	}
	if len(loaded.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(loaded.Snapshots))
	}
	if loaded.Snapshots[0].Entries["/Applications"] != 20<<30 {
		t.Fatalf("mismatched snapshot 1 size: %d", loaded.Snapshots[0].Entries["/Applications"])
	}
}

func TestFindBestBaselineSnapshot(t *testing.T) {
	now := time.Now()
	history := &TrendHistory{
		Snapshots: []TrendSnapshot{
			{
				Timestamp: now.Add(-14 * 24 * time.Hour),
				Target:    "/",
				Entries:   map[string]int64{"/a": 100},
			},
			{
				Timestamp: now.Add(-7 * 24 * time.Hour),
				Target:    "/",
				Entries:   map[string]int64{"/a": 200},
			},
			{
				Timestamp: now.Add(-1 * time.Minute), // too recent
				Target:    "/",
				Entries:   map[string]int64{"/a": 300},
			},
			{
				Timestamp: now.Add(-7 * 24 * time.Hour),
				Target:    "/other", // different target
				Entries:   map[string]int64{"/b": 500},
			},
		},
	}

	best := findBestBaselineSnapshot(history, "/", 7, now)
	if best == nil {
		t.Fatalf("expected to find a 7-day baseline snapshot")
	}
	if best.Entries["/a"] != 200 {
		t.Fatalf("expected snapshot with /a=200, got %d", best.Entries["/a"])
	}

	best14 := findBestBaselineSnapshot(history, "/", 14, now)
	if best14 == nil || best14.Entries["/a"] != 100 {
		t.Fatalf("expected 14-day baseline snapshot with /a=100")
	}
}

func TestAnalyzeDirectory(t *testing.T) {
	tests := []struct {
		path         string
		current      int64
		delta        int64
		wantCategory string
	}{
		{"/Users/test/Library/Developer/Xcode/DerivedData", 10 << 30, 5 << 30, "开发构建派生"},
		{"/Users/test/Library/Caches/Google/Chrome", 2 << 30, 1 << 30, "运行时缓存"},
		{"/Users/test/Downloads", 15 << 30, 3 << 30, "用户下载目录"},
		{"/Users/test/.cache/pnpm/store", 8 << 30, 2 << 30, "依赖与包缓存"},
		{"/Users/test/.ollama/models", 20 << 30, 10 << 30, "AI 模型权重"},
		{"/Users/test/Library/Application Support/MobileSync/Backup/123", 30 << 30, 15 << 30, "设备本地备份"},
		{"/Users/test/Library/Containers/com.tencent.xinWeChat", 12 << 30, 4 << 30, "沙盒应用数据"},
		{"/Users/test/SomeHugeDir", 50 << 30, 10 << 30, "空间显著增长"},
		{"/Users/test/NormalDir", 1 << 30, 100 << 20, "常规数据目录"},
	}

	for _, tt := range tests {
		cat, reason, sug := analyzeDirectory(tt.path, tt.delta)
		if cat != tt.wantCategory {
			t.Errorf("analyzeDirectory(%q) category = %q, want %q", tt.path, cat, tt.wantCategory)
		}
		if reason == "" || sug == "" {
			t.Errorf("analyzeDirectory(%q) reason or suggestion is empty", tt.path)
		}
	}
}

func TestPrintTrendReportInitialAndDiff(t *testing.T) {
	// 1. Initial report test
	initReport := TrendReport{
		Target:       "/",
		IsOverview:   true,
		IsInitial:    true,
		TotalCurrent: 30 << 30,
		Items: []TrendComparisonItem{
			{
				Name:        "Applications",
				Path:        "/Applications",
				CurrentSize: 20 << 30,
				Category:    "常规数据目录",
			},
			{
				Name:        "Xcode DerivedData",
				Path:        "/Users/test/Library/Developer/Xcode/DerivedData",
				CurrentSize: 10 << 30,
				Category:    "开发构建派生",
			},
		},
	}

	// Just ensure printTrendReport does not crash
	printTrendReport(initReport)

	// 2. Diff report test
	baseTime := time.Now().Add(-7 * 24 * time.Hour)
	diffReport := TrendReport{
		Target:       "/",
		IsOverview:   true,
		IsInitial:    false,
		BaseTime:     &baseTime,
		DaysAgo:      7.0,
		TotalCurrent: 35 << 30,
		TotalDelta:   5 << 30,
		Items: []TrendComparisonItem{
			{
				Name:        "Xcode DerivedData",
				Path:        "/Users/test/Library/Developer/Xcode/DerivedData",
				CurrentSize: 15 << 30,
				PrevSize:    10 << 30,
				Delta:       5 << 30,
				GrowthPct:   50.0,
				Category:    "开发构建派生",
				Reason:      "Xcode 编译产物",
				Suggestion:  "可运行 mo clean 清理",
			},
			{
				Name:        "Applications",
				Path:        "/Applications",
				CurrentSize: 20 << 30,
				PrevSize:    20 << 30,
				Delta:       0,
				GrowthPct:   0.0,
				Category:    "常规数据目录",
				Reason:      "正常使用",
				Suggestion:  "按需保持关注",
			},
		},
	}
	printTrendReport(diffReport)
}

func TestParseArgsTrendFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code, keepGoing := parseArgs([]string{"--trend", "--days", "14"}, &stdout, &stderr)
	if code != 0 || !keepGoing {
		t.Fatalf("expected parseArgs to succeed, code=%d, keepGoing=%v, stderr=%s", code, keepGoing, stderr.String())
	}
	if !*trendMode {
		t.Fatalf("expected trendMode to be true")
	}
	if *trendDays != 14 {
		t.Fatalf("expected trendDays to be 14, got %d", *trendDays)
	}
}

func TestParseArgsHelpIncludesTrend(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code, keepGoing := parseArgs([]string{"--help"}, &stdout, &stderr)
	if code != 0 || keepGoing {
		t.Fatalf("expected exit 0 and stop, code=%d, keepGoing=%v", code, keepGoing)
	}
	if !strings.Contains(stdout.String(), "--trend") {
		t.Fatalf("expected usageText to contain --trend, got:\n%s", stdout.String())
	}
}
