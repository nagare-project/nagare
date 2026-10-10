package mpv

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

// Anime4K 实时超分（[bloc97/Anime4K](https://github.com/bloc97/Anime4K)，修订 7684e95）。
//
// 着色器随二进制分发（anime4k/ 目录）。许可逐个文件写在文件头：AutoDownscalePre_x2/x4 是 Unlicense
// （公有领域），其余是 MIT（anime4k/LICENSE）；汇总见 THIRD_PARTY_NOTICES.md。
// 播放时写进运行时目录交给 mpv。只带官方 mpv 模板里的两套 Mode A 组合（为 1080p 动画优化，
// 720p 源同样适用；文件名与顺序照抄 md/Template/GLSL_*/mpv.conf）：
//   - fast：Mode A (Fast)，官方给核显 / 低端独显的那套
//   - hq：Mode A (HQ)，官方给中高端独显的那套；算力不够会掉帧
//
// 着色器要 mpv 的 gpu / gpu-next 视频输出才生效，mpv 默认就是。
const (
	Anime4KOff  = ""
	Anime4KFast = "fast"
	Anime4KHQ   = "hq"
)

//go:embed anime4k/*.glsl
var anime4kFiles embed.FS

var anime4kPresets = map[string][]string{
	Anime4KFast: {
		"Anime4K_Clamp_Highlights.glsl",
		"Anime4K_Restore_CNN_M.glsl",
		"Anime4K_Upscale_CNN_x2_M.glsl",
		"Anime4K_AutoDownscalePre_x2.glsl",
		"Anime4K_AutoDownscalePre_x4.glsl",
		"Anime4K_Upscale_CNN_x2_S.glsl",
	},
	Anime4KHQ: {
		"Anime4K_Clamp_Highlights.glsl",
		"Anime4K_Restore_CNN_VL.glsl",
		"Anime4K_Upscale_CNN_x2_VL.glsl",
		"Anime4K_AutoDownscalePre_x2.glsl",
		"Anime4K_AutoDownscalePre_x4.glsl",
		"Anime4K_Upscale_CNN_x2_M.glsl",
	},
}

// ValidAnime4K 报告预设名认不认识（空串 = 关）。
func ValidAnime4K(preset string) bool {
	if preset == Anime4KOff {
		return true
	}
	_, ok := anime4kPresets[preset]
	return ok
}

// Anime4KShaders 把预设用到的着色器写进 dir/anime4k/，按 mpv 应用的顺序返回绝对路径；关（空串）返回 nil。
//
// 每次起播都会调：盘上已有且内容一致的不重写。着色器只是 GLSL 文本，写坏了 mpv 只会报着色器编译失败，
// 不影响播放本身 —— 但还是先写临时文件再改名，别让 mpv 读到半截文件。
func Anime4KShaders(dir, preset string) ([]string, error) {
	names, ok := anime4kPresets[preset]
	if !ok {
		if preset == Anime4KOff {
			return nil, nil
		}
		return nil, fmt.Errorf("未知的 Anime4K 预设：%q", preset)
	}
	target := filepath.Join(dir, "anime4k")
	if err := os.MkdirAll(target, 0o700); err != nil {
		return nil, fmt.Errorf("创建着色器目录: %w", err)
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		content, err := anime4kFiles.ReadFile("anime4k/" + name)
		if err != nil {
			return nil, fmt.Errorf("读取内置着色器 %s: %w", name, err)
		}
		path := filepath.Join(target, name)
		if existing, err := os.ReadFile(path); err != nil || !bytes.Equal(existing, content) {
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, content, 0o600); err != nil {
				return nil, fmt.Errorf("写出着色器 %s: %w", name, err)
			}
			if err := os.Rename(tmp, path); err != nil {
				_ = os.Remove(tmp)
				return nil, fmt.Errorf("写出着色器 %s: %w", name, err)
			}
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, abs)
	}
	return paths, nil
}

// SetShaders 换掉正在播放的这个窗口的着色器（空列表 = 全部清掉），不用重开 mpv。
func (p *Player) SetShaders(paths []string) error {
	if paths == nil {
		paths = []string{}
	}
	return p.SetProperty("glsl-shaders", paths)
}
