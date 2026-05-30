package docgen

import (
	"os"
	"strings"
	"testing"
)

func TestBuildPrintHTML(t *testing.T) {
	wiki := &Wiki{
		ProjectName:     "TestProj",
		Overview:        "# 项目概述\n\n这是一个测试项目。\n\n## 背景\n\n项目起源于测试需求。",
		WhatItDoes:      "## 能做什么\n\n- 打印 Hello\n- 退出程序",
		Architecture:    "## 架构\n\n单文件架构。",
		ProjectStructure: "## 结构\n\n- main.go",
		LearningPath:    "## 学习路径\n\n1. 阅读 main.go\n2. 运行程序",
		ModuleThemes: map[string][]string{
			"core": {"main"},
		},
		ChapterTitles: map[string]ChapterTitle{
			"core": {Title: "核心模块", Subtitle: "程序入口与主逻辑", Difficulty: "⭐ 入门"},
		},
		ChapterNarratives: map[string]string{
			"core": "# 核心模块\n\n`main` 包是整个程序的入口。",
		},
		ModuleDocs: map[string]string{
			"main": "# main\n\n程序入口。",
		},
	}

	html, err := buildPrintHTML(wiki, nil)
	if err != nil {
		t.Fatalf("buildPrintHTML failed: %v", err)
	}

	// Ensure essential sections exist
	required := []string{
		"项目概述", "项目能做什么", "架构说明", "项目结构",
		"学习路径", "核心模块", "目录", "TestProj",
	}
	for _, s := range required {
		if !strings.Contains(html, s) {
			t.Errorf("missing expected content: %q", s)
		}
	}

	// Ensure print CSS contains background-clip fallback
	if !strings.Contains(html, "-webkit-text-fill-color") {
		t.Error("print CSS missing background-clip fallback")
	}

	// Ensure page-break styles exist
	if !strings.Contains(html, "page-break-before: always") {
		t.Error("missing page-break-before style")
	}

	// Write out for manual inspection
	_ = os.WriteFile("test_print.html", []byte(html), 0644)
}

func TestFindChrome(t *testing.T) {
	// Just ensure it doesn't panic and returns either a path or an error
	path, err := findChrome()
	if err == nil && path == "" {
		t.Error("findChrome returned nil error but empty path")
	}
	if err != nil {
		t.Logf("findChrome returned error (expected if no Chrome): %v", err)
	} else {
		t.Logf("findChrome found: %s", path)
	}
}
