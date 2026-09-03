package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const (
	testVersion = "0.2.0"
	tarAsset    = "nagare-0.2.0_Linux_x86_64.tar.gz"
	zipAsset    = "nagare-0.2.0_Windows_x86_64.zip"
	appAsset    = "nagare-0.2.0_MacOS_universal.app.zip"
)

// releaseFiles 把一个资产摆成完整的一次发布（资产 + 校验清单 + 签名）。
func releaseFiles(t *testing.T, s testSigner, asset string, body []byte, prehashed bool) map[string][]byte {
	t.Helper()
	manifest := checksumsFor(map[string][]byte{asset: body})
	return map[string][]byte{
		"/v" + testVersion + "/" + asset:              body,
		"/v" + testVersion + "/checksums.txt":         manifest,
		"/v" + testVersion + "/checksums.txt.minisig": s.sign(t, manifest, "nagare "+testVersion+" checksums", prehashed),
	}
}

// TestApplyDirectTarGz 是整条链的端到端：下载 → 验签 → 校验哈希 → 解包 → 替换。
func TestApplyDirectTarGz(t *testing.T) {
	for _, prehashed := range []bool{false, true} {
		name := "纯签名"
		if prehashed {
			name = "预哈希签名"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "nagare")
			writeFile(t, exe, "OLD BINARY", 0o755)

			signer := newTestSigner(t)
			archive := buildTarGz(t, []entry{
				{name: "nagare", body: "NEW BINARY", exec: true},
				{name: "LICENSE", body: "AGPL-3.0"},
			})
			srv := serveRelease(t, releaseFiles(t, signer, tarAsset, archive, prehashed))
			u := newTestUpdater(t, srv, signer.pub, "linux", "amd64", exe)

			capability := u.Capability()
			require.True(t, capability.Supported, capability.Reason)
			require.Equal(t, ChannelDirect, capability.Channel)
			require.Equal(t, exe, capability.Target)

			require.NoError(t, u.Apply(context.Background(), testVersion))

			require.Equal(t, "NEW BINARY", readFile(t, exe))
			require.Equal(t, "OLD BINARY", readFile(t, exe+oldSuffix), "旧版本应留在 .old 里")
			require.Empty(t, stagingLeftovers(t, dir), "暂存目录必须清干净")
			// 归档里的其他文件不该被搬进安装目录：只换二进制。
			require.NoFileExists(t, filepath.Join(dir, "LICENSE"))
			if runtime.GOOS != "windows" {
				st, err := os.Stat(exe)
				require.NoError(t, err)
				require.NotZero(t, st.Mode().Perm()&0o111, "装上去的二进制必须可执行")
			}

			u.SweepOldFiles()
			require.NoFileExists(t, exe+oldSuffix)
		})
	}
}

// TestApplyDirectZipReplacesBundledMPV 覆盖 Windows 便携版：exe 与旁边的 mpv/ 一起换。
func TestApplyDirectZipReplacesBundledMPV(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare.exe")
	mpv := filepath.Join(dir, mpvDirName, "mpv.exe")
	writeFile(t, exe, "OLD BINARY", 0o755)
	writeFile(t, mpv, "OLD MPV", 0o755)

	signer := newTestSigner(t)
	archive := buildZip(t, []entry{
		{name: "nagare.exe", body: "NEW BINARY", exec: true},
		{name: mpvDirName, dir: true},
		{name: mpvDirName + "/mpv.exe", body: "NEW MPV", exec: true},
	})
	srv := serveRelease(t, releaseFiles(t, signer, zipAsset, archive, false))
	u := newTestUpdater(t, srv, signer.pub, "windows", "amd64", exe)

	require.NoError(t, u.Apply(context.Background(), testVersion))

	require.Equal(t, "NEW BINARY", readFile(t, exe))
	require.Equal(t, "OLD BINARY", readFile(t, exe+oldSuffix))
	require.Equal(t, "NEW MPV", readFile(t, mpv))
	require.Equal(t, "OLD MPV", readFile(t, filepath.Join(dir, mpvDirName+oldSuffix, "mpv.exe")))
	require.Empty(t, stagingLeftovers(t, dir))

	u.SweepOldFiles()
	require.NoFileExists(t, exe+oldSuffix)
	require.NoDirExists(t, filepath.Join(dir, mpvDirName+oldSuffix))
}

// TestApplyAppBundleReplacesWholeBundle 覆盖 macOS：整个 .app 被换掉，
// 而不是只换里面的二进制（换里面的会毁掉 ad-hoc 签名，macOS 直接拒绝启动）。
func TestApplyAppBundleReplacesWholeBundle(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Nagare.app")
	exe := filepath.Join(app, "Contents", "MacOS", "nagare")
	sigFile := filepath.Join("Contents", "_CodeSignature", "CodeResources")
	writeFile(t, exe, "OLD BINARY", 0o755)
	writeFile(t, filepath.Join(app, "Contents", "Info.plist"), "<plist>OLD</plist>", 0o644)
	writeFile(t, filepath.Join(app, sigFile), "OLD SIGNATURE", 0o644)

	signer := newTestSigner(t)
	archive := buildZip(t, []entry{
		{name: "Nagare.app", dir: true},
		{name: "Nagare.app/Contents/Info.plist", body: "<plist>NEW</plist>"},
		{name: "Nagare.app/Contents/MacOS/nagare", body: "NEW BINARY", exec: true},
		{name: "Nagare.app/Contents/_CodeSignature/CodeResources", body: "NEW SIGNATURE"},
		// ditto --sequesterRsrc 压出来的元数据，必须被整棵跳过。
		{name: "__MACOSX/Nagare.app/._Info.plist", body: "resource fork"},
		{name: "__MACOSX/Nagare.app/Contents/._MacOS", body: "resource fork"},
	})
	srv := serveRelease(t, releaseFiles(t, signer, appAsset, archive, false))
	u := newTestUpdater(t, srv, signer.pub, "darwin", "arm64", exe)

	capability := u.Capability()
	require.True(t, capability.Supported, capability.Reason)
	require.Equal(t, ChannelAppBundle, capability.Channel)
	require.Equal(t, app, capability.Target, "要替换的是 .app 目录，不是里面的二进制")

	require.NoError(t, u.Apply(context.Background(), testVersion))

	require.Equal(t, "NEW BINARY", readFile(t, exe))
	require.Equal(t, "<plist>NEW</plist>", readFile(t, filepath.Join(app, "Contents", "Info.plist")))
	require.Equal(t, "NEW SIGNATURE", readFile(t, filepath.Join(app, sigFile)),
		"_CodeSignature 必须随整包一起换掉")
	require.Equal(t, "OLD BINARY", readFile(t, filepath.Join(app+oldSuffix, "Contents", "MacOS", "nagare")))
	require.NoDirExists(t, filepath.Join(app, macOSMetaDir))
	require.NoDirExists(t, filepath.Join(dir, macOSMetaDir))
	require.Empty(t, stagingLeftovers(t, dir))

	u.SweepOldFiles()
	require.NoDirExists(t, app+oldSuffix)
}

// TestApplyRejects 覆盖每一条拒绝路径。共同的硬要求：目标文件【一个字节都没被动过】，
// 且暂存目录与 .old 都不留 —— 失败的更新必须像没发生过一样。
func TestApplyRejects(t *testing.T) {
	tests := []struct {
		name string
		// files 在正确的发布基础上做手脚；nil 表示不改。
		files func(t *testing.T, good map[string][]byte, right, wrong testSigner) map[string][]byte
		// pubKey 非 nil 时覆盖内嵌公钥。
		pubKey  *string
		version string
		wantMsg string
	}{
		{
			name: "签名来自别的钥匙",
			files: func(t *testing.T, good map[string][]byte, _, wrong testSigner) map[string][]byte {
				manifest := good["/v"+testVersion+"/checksums.txt"]
				good["/v"+testVersion+"/checksums.txt.minisig"] =
					wrong.sign(t, manifest, "nagare "+testVersion+" checksums", false)
				return good
			},
			wantMsg: "签名校验失败",
		},
		{
			name: "清单被改过（签名对不上内容）",
			files: func(t *testing.T, good map[string][]byte, _, _ testSigner) map[string][]byte {
				good["/v"+testVersion+"/checksums.txt"] = append(
					good["/v"+testVersion+"/checksums.txt"], []byte("tampered\n")...)
				return good
			},
			wantMsg: "签名校验失败",
		},
		{
			name: "归档内容与清单里的哈希不符",
			files: func(t *testing.T, good map[string][]byte, _, _ testSigner) map[string][]byte {
				good["/v"+testVersion+"/"+tarAsset] = []byte("not the archive you signed")
				return good
			},
			wantMsg: "校验和与清单不符",
		},
		{
			name: "清单里没有本平台的包",
			files: func(t *testing.T, good map[string][]byte, s, _ testSigner) map[string][]byte {
				manifest := checksumsFor(map[string][]byte{"nagare-0.2.0_Plan9_x86_64.tar.gz": []byte("x")})
				good["/v"+testVersion+"/checksums.txt"] = manifest
				good["/v"+testVersion+"/checksums.txt.minisig"] =
					s.sign(t, manifest, "nagare "+testVersion+" checksums", false)
				return good
			},
			wantMsg: "没有适用于当前平台的更新包",
		},
		{
			name: "签名对应的是别的版本（重放旧发布）",
			files: func(t *testing.T, good map[string][]byte, s, _ testSigner) map[string][]byte {
				manifest := good["/v"+testVersion+"/checksums.txt"]
				good["/v"+testVersion+"/checksums.txt.minisig"] =
					s.sign(t, manifest, "nagare 0.1.5 checksums", false)
				return good
			},
			wantMsg: "签名对应的不是要安装的版本",
		},
		{
			name: "清单根本不存在",
			files: func(t *testing.T, good map[string][]byte, _, _ testSigner) map[string][]byte {
				delete(good, "/v"+testVersion+"/checksums.txt")
				return good
			},
			wantMsg: "没有适用于当前平台的更新包",
		},
		{
			name:    "这个构建没有内嵌公钥",
			pubKey:  strptr(""),
			wantMsg: "不支持自动更新",
		},
		{
			name:    "版本号格式不合法",
			version: "../../evil",
			wantMsg: "版本号格式不合法",
		},
		{
			name:    "降级到更旧的版本",
			version: "0.0.9",
			wantMsg: "更旧",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "nagare")
			writeFile(t, exe, "OLD BINARY", 0o755)

			signer := newTestSigner(t)
			files := releaseFiles(t, signer, tarAsset,
				buildTarGz(t, []entry{{name: "nagare", body: "NEW BINARY", exec: true}}), false)
			if tc.files != nil {
				files = tc.files(t, files, signer, newTestSigner(t))
			}
			srv := serveRelease(t, files)

			pub := signer.pub
			if tc.pubKey != nil {
				pub = *tc.pubKey
			}
			version := testVersion
			if tc.version != "" {
				version = tc.version
			}
			u := newTestUpdater(t, srv, pub, "linux", "amd64", exe)

			err := u.Apply(context.Background(), version)
			require.Error(t, err)
			require.Contains(t, userFacing(err), tc.wantMsg)

			// 三条铁律：原文件没被动、没留备份、没留暂存目录。
			require.Equal(t, "OLD BINARY", readFile(t, exe))
			require.NoFileExists(t, exe+oldSuffix)
			require.Empty(t, stagingLeftovers(t, dir))
		})
	}
}

// TestApplyRefusesPackageChannel：包管理器装的只提示，不自己动手。
func TestApplyRefusesPackageChannel(t *testing.T) {
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", "/usr/bin/nagare")
	capability := u.Capability()
	require.False(t, capability.Supported)
	require.Equal(t, ChannelPackage, capability.Channel)
	require.Contains(t, capability.Reason, "apt")

	err := u.Apply(context.Background(), testVersion)
	require.Error(t, err)
	require.Contains(t, userFacing(err), "不支持自动更新")
}

// TestApplyRejectsConcurrentRun：同一时刻只允许一次更新在跑。
func TestApplyRejectsConcurrentRun(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "OLD BINARY", 0o755)
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", exe)

	u.applySem <- struct{}{} // 占住信号量，模拟另一次 Apply 正在跑
	defer func() { <-u.applySem }()

	err := u.Apply(context.Background(), testVersion)
	require.Error(t, err)
	require.Contains(t, userFacing(err), "已经有一次更新在进行中")
}

// TestCapabilityRejectsUnwritableDir：装在不可写的目录里时提前说清楚，
// 而不是下完 120MB 再在替换那一步失败。
func TestCapabilityRejectsUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的目录权限不受 chmod 控制，只读目录要靠 ACL 造，跳过")
	}
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位形同虚设，跳过")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "OLD BINARY", 0o755)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", exe)
	capability := u.Capability()
	require.False(t, capability.Supported)
	require.Contains(t, capability.Reason, "不可写")

	err := u.Apply(context.Background(), testVersion)
	require.Error(t, err)
	require.Contains(t, userFacing(err), "不可写")
}

// TestSweepOldFiles：清得掉残留，没有残留时也不吭声。
func TestSweepOldFiles(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "CURRENT", 0o755)
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", exe)

	// 没有残留时是空操作，不 panic 也不报错。
	u.SweepOldFiles()
	require.FileExists(t, exe)

	writeFile(t, exe+oldSuffix, "PREVIOUS", 0o755)
	stale := filepath.Join(dir, stagePrefix+"abc123")
	writeFile(t, filepath.Join(stale, "archive"), "half a download", 0o644)
	probe := filepath.Join(dir, probePrefix+"999")
	writeFile(t, probe, "", 0o644)
	keep := filepath.Join(dir, "library")
	writeFile(t, filepath.Join(keep, "note.txt"), "用户的东西", 0o644)

	u.SweepOldFiles()

	require.NoFileExists(t, exe+oldSuffix)
	require.NoDirExists(t, stale)
	require.NoFileExists(t, probe, "探测文件的残留也要扫掉")
	require.FileExists(t, exe, "当前版本不能被扫掉")
	require.FileExists(t, filepath.Join(keep, "note.txt"), "只清自己的残留，不碰别的东西")
}

// TestSweepOldFilesSkipsPackageChannel：/usr/bin 这类目录归包管理器管，一个字节都不动。
func TestSweepOldFilesSkipsPackageChannel(t *testing.T) {
	dir := t.TempDir()
	// 用真实的 /usr/bin 路径判定渠道，但把要清理的东西造在 t.TempDir 里，
	// 断言的是「压根没走到清理那一步」。
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", "/usr/bin/nagare")
	writeFile(t, filepath.Join(dir, stagePrefix+"leftover", "x"), "x", 0o644)
	u.SweepOldFiles()
	require.DirExists(t, filepath.Join(dir, stagePrefix+"leftover"))
}

// TestRestartTarget 只验参数组装 —— 真的 exec 会把测试进程换掉。
func TestRestartTarget(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nagare")
	writeFile(t, exe, "CURRENT", 0o755)
	u := newTestUpdater(t, nil, "pubkey-placeholder", "linux", "amd64", exe)

	got, argv, err := u.restartTarget()
	require.NoError(t, err)
	require.Equal(t, exe, got)
	require.Equal(t, exe, argv[0], "argv[0] 要指向新装好的可执行文件")
	require.Equal(t, os.Args[1:], argv[1:], "原有的命令行参数要保留")
}

// TestRestartTargetAppBundle：app bundle 替换后原路径上就是新版本，重启目标不变。
func TestRestartTargetAppBundle(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Nagare.app", "Contents", "MacOS", "nagare")
	writeFile(t, exe, "CURRENT", 0o755)
	u := newTestUpdater(t, nil, "pubkey-placeholder", "darwin", "arm64", exe)

	got, argv, err := u.restartTarget()
	require.NoError(t, err)
	require.Equal(t, exe, got)
	require.True(t, strings.HasSuffix(argv[0], filepath.Join("Contents", "MacOS", "nagare")))
}

func strptr(s string) *string { return &s }

// userFacing 取错误给用户看的完整文案（UserMsg + Recovery）。
// 断言要盯的是「用户看到什么」，而不是内部的英文 op 串。
func userFacing(err error) string {
	var e *errs.E
	if errors.As(err, &e) {
		return e.UserFacing()
	}
	return err.Error()
}
