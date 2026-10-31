// gentokens 从 design/tokens.json 生成三端的设计 token 与 DESIGN.md 里的数值表：
//
//   - frontend/src/styles/tokens.css           桌面网页（CSS 自定义属性）
//   - ios/Tokens/Tokens.swift                  iOS（系统语义色引用 + 固定值）
//   - internal/mpv/ui/scripts/nagare/tokens.lua 播放器界面（mpv Lua）
//   - DESIGN.md 里 <!-- gentokens:begin … --> 标记之间的表格
//
// 由仓库根目录 generate.go 里的 go:generate 调用：在根目录运行 go generate ./...。
// CI 跑一遍再 git diff --exit-code，生成物与 tokens.json 不一致就红。
// 只用标准库，不加依赖。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// 相对仓库根目录的路径。
const (
	tokensPath = "design/tokens.json"
	designPath = "DESIGN.md"
	cssPath    = "frontend/src/styles/tokens.css"
	swiftPath  = "ios/Tokens/Tokens.swift"
	luaPath    = "internal/mpv/ui/scripts/nagare/tokens.lua"
)

// outputPaths 是生成器写的全部文件（DESIGN.md 只改标记之间的部分）。
var outputPaths = []string{cssPath, swiftPath, luaPath, designPath}

func main() {
	root := flag.String("root", ".", "仓库根目录")
	flag.Parse()
	if err := run(*root); err != nil {
		fmt.Fprintln(os.Stderr, "gentokens:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	tokens, err := os.ReadFile(filepath.Join(root, tokensPath))
	if err != nil {
		return err
	}
	design, err := os.ReadFile(filepath.Join(root, designPath))
	if err != nil {
		return err
	}
	outs, err := render(tokens, design)
	if err != nil {
		return err
	}
	for _, rel := range outputPaths {
		if err := writeIfChanged(filepath.Join(root, filepath.FromSlash(rel)), outs[rel]); err != nil {
			return fmt.Errorf("写 %s: %w", rel, err)
		}
	}
	return nil
}

// render 是纯函数：tokens.json 与当前 DESIGN.md 进，各个生成物（按相对路径）出。
// 黄金测试与「仓库里的生成物是最新的」测试都直接调它。
func render(tokens, design []byte) (map[string][]byte, error) {
	f, err := decodeTokens(tokens)
	if err != nil {
		return nil, err
	}
	doc, err := renderDesign(design, markdownBlocks(f))
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		cssPath:    renderCSS(f),
		swiftPath:  renderSwift(f),
		luaPath:    renderLua(f),
		designPath: doc,
	}, nil
}

// writeIfChanged 内容相同就不写，免得 go generate 无谓地改 mtime。
func writeIfChanged(path string, content []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
