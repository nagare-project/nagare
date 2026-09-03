package selfupdate

// 本文件是发布产物命名与下载地址的唯一出处。名字必须与 .goreleaser.yaml 的
// archives.name_template 及 scripts/macos/build-app.sh 的产物名逐字一致 ——
// 对不上的后果是「有新版本但更新不了」，且只有在真发一次版之后才会暴露。

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const (
	// checksumsName 是校验清单（goreleaser 的 checksum.name_template）。
	checksumsName = "checksums.txt"
	// minisigExt 是 minisign 签名的后缀（goreleaser signs.signature）。
	minisigExt = ".minisig"
)

// assetTemplate 返回本平台/本渠道要下载的归档名模板，%s 处填版本号。
//
// macOS 的 app bundle 走单独的 .app.zip 而不是裸二进制的 tar.gz：bundle 是 ad-hoc
// 签名的，只换里面那个可执行文件会让签名失效，macOS 直接拒绝启动（见 install.go）。
func assetTemplate(ch Channel, goos, goarch string) (string, error) {
	const op = "selfupdate.asset"
	unsupported := func() (string, error) {
		return "", errs.New(errs.CategoryInput, op,
			fmt.Sprintf("当前平台（%s/%s）没有可用的更新包", goos, goarch),
			"请到项目发布页确认是否有对应平台的版本")
	}
	if ch == ChannelAppBundle {
		// scripts/macos/build-app.sh 的产物：ditto --keepParent 压的整个 Nagare.app。
		return "nagare-%s_MacOS_universal.app.zip", nil
	}
	switch goos {
	case "darwin":
		// 三个 arch 共用一个 universal 归档（.goreleaser.yaml 的 universal_binaries）。
		return "nagare-%s_MacOS_universal.tar.gz", nil
	case "linux":
		switch goarch {
		case "amd64":
			return "nagare-%s_Linux_x86_64.tar.gz", nil
		case "arm64":
			return "nagare-%s_Linux_arm64.tar.gz", nil
		}
		return unsupported()
	case "windows":
		if goarch == "amd64" {
			return "nagare-%s_Windows_x86_64.zip", nil
		}
		return unsupported()
	}
	return unsupported()
}

// assetName 是填好版本号的归档名。
func assetName(ch Channel, goos, goarch, version string) (string, error) {
	tmpl, err := assetTemplate(ch, goos, goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(tmpl, version), nil
}

// assetURL 拼出一个发布资产的下载地址：<base>/v<版本>/<文件名>。
// 版本号已经过 normalizeVersion 的形状校验，不可能含路径穿越。
func (u *Updater) assetURL(version, name string) string {
	return u.baseURL + "/v" + url.PathEscape(version) + "/" + url.PathEscape(name)
}

// resolveBaseURL 定下载前缀：显式参数 > 环境变量 > 按仓库推出的 GitHub 地址。
//
// 允许覆盖是分发风险的缓解措施（Release 资产被投诉下架时要能立刻改指向），
// 但必须是 https —— 更新包的完整性靠 minisign 保证，机密性与「是不是真的连上了
// 声称的那台主机」仍然要靠 TLS。明文 http 的覆盖一律拒绝，不降级、不警告放行。
func resolveBaseURL(explicit, repo string) (string, error) {
	const op = "selfupdate.baseurl"
	raw := strings.TrimSpace(explicit)
	source := "BaseURL 参数"
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(EnvBaseURL))
		source = EnvBaseURL
	}
	if raw == "" {
		return "https://github.com/" + repo + "/releases/download", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", errs.New(errs.CategoryInput, op,
			source+" 不是合法的地址", "请填写形如 https://example.com/releases 的下载前缀")
	}
	if parsed.Scheme != "https" {
		return "", errs.New(errs.CategoryInput, op,
			source+" 必须是 https 地址", "明文 http 的更新源不被接受，请改用 https")
	}
	return strings.TrimSuffix(raw, "/"), nil
}
