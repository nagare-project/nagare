package selfupdate

// 归档来自网络，即使已经过签名与哈希校验，解包时仍然按【不可信输入】处理：
// 签名只证明「这份字节是发布方给的」，证明不了「发布方的构建机没被人塞进一条
// ../../../.ssh/authorized_keys」。这一层的成本很低，省掉它的收益是零。
//
// 规则：只接受普通文件与目录；路径必须落在目标目录之内；单条与总量都有上限；
// 权限只保留可执行位。软链接、硬链接、设备、FIFO 一律拒绝 —— 更新包里不该有这些，
// 出现即视为构造过的归档。

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
)

const (
	// maxEntryBytes 是单个条目的上限（Windows 包里最大的是 mpv.exe，约 100MB）。
	maxEntryBytes = 256 << 20
	// maxTotalBytes 是解包总量上限，挡 zip bomb。
	maxTotalBytes = 768 << 20
	// maxEntries 是条目数上限，挡「一百万个空文件」那种把 inode 耗光的归档。
	maxEntries = 20000
	// dirMode / fileMode / execMode 是解出来的固定权限。
	dirMode  fs.FileMode = 0o755
	fileMode fs.FileMode = 0o644
	execMode fs.FileMode = 0o755
	// macOSMetaDir 是 ditto --sequesterRsrc 压出来的元数据目录，整棵跳过。
	// Go 的 archive/zip 本来也还原不了扩展属性，留着只会在 bundle 里多出一堆垃圾。
	macOSMetaDir = "__MACOSX"
)

// extractArchive 按归档名的后缀分派解包器，解到 dst（会被创建）。
func extractArchive(src, dst, name string) error {
	const op = "selfupdate.extract"
	if err := os.MkdirAll(dst, dirMode); err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "创建解包目录失败",
			"请确认磁盘空间充足且安装目录可写", err)
	}
	e := newExtractor(dst)
	switch {
	case strings.HasSuffix(name, ".tar.gz"):
		return e.tarGz(src)
	case strings.HasSuffix(name, ".zip"): // 含 .app.zip
		return e.zip(src)
	default:
		return errs.New(errs.CategoryInternal, op,
			"无法识别的更新包格式，已中止更新", "请到项目发布页手动下载")
	}
}

// extractor 累计条目数与总字节数，供上限判断。
// 三个上限做成字段而不是直接引用常量：测试要用很小的值去撞它们，
// 否则验证「256MB 的条目会被拒」就得真的造一个 256MB 的条目。
type extractor struct {
	dst      string
	maxEntry int64
	maxTotal int64
	maxCount int

	total int64
	count int
}

func newExtractor(dst string) *extractor {
	return &extractor{dst: dst, maxEntry: maxEntryBytes, maxTotal: maxTotalBytes, maxCount: maxEntries}
}

func (e *extractor) tarGz(src string) error {
	const op = "selfupdate.extract"
	f, err := os.Open(src)
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "打开更新包失败", "请重试更新", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return badArchive(op, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return badArchive(op, err)
		}
		if err := e.tarEntry(hdr, tr); err != nil {
			return err
		}
	}
}

func (e *extractor) tarEntry(hdr *tar.Header, tr *tar.Reader) error {
	const op = "selfupdate.extract"
	switch hdr.Typeflag {
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		// PAX 元数据，不是内容条目。
		return nil
	case tar.TypeDir, tar.TypeReg:
	default:
		// 软链接（TypeSymlink）、硬链接（TypeLink）、设备、FIFO 都落在这里。
		return rejected(op, hdr.Name, fmt.Sprintf("类型 %q 不被接受", string(hdr.Typeflag)))
	}
	path, skip, err := e.entryPath(hdr.Name)
	if err != nil || skip {
		return err
	}
	if hdr.Typeflag == tar.TypeDir {
		return e.mkdir(path)
	}
	if hdr.Size > e.maxEntry {
		return rejected(op, hdr.Name, "条目超过单条大小上限")
	}
	return e.writeFile(path, tr, modeFor(hdr.FileInfo().Mode()))
}

func (e *extractor) zip(src string) error {
	const op = "selfupdate.extract"
	zr, err := zip.OpenReader(src)
	if err != nil {
		return badArchive(op, err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if err := e.zipEntry(f); err != nil {
			return err
		}
	}
	return nil
}

func (e *extractor) zipEntry(f *zip.File) error {
	const op = "selfupdate.extract"
	info := f.FileInfo()
	// IsRegular 只在类型位全零时为真：软链接、设备、FIFO 都会被这一句挡掉。
	if !info.IsDir() && !f.Mode().IsRegular() {
		return rejected(op, f.Name, "只接受普通文件与目录")
	}
	path, skip, err := e.entryPath(f.Name)
	if err != nil || skip {
		return err
	}
	if info.IsDir() {
		return e.mkdir(path)
	}
	// UncompressedSize64 由归档自己声明（可以撒谎），只当快速否决用；
	// 真正的上限由 writeFile 里的 LimitReader 兜住。
	if f.UncompressedSize64 > uint64(e.maxEntry) {
		return rejected(op, f.Name, "条目超过单条大小上限")
	}
	rc, err := f.Open()
	if err != nil {
		return badArchive(op, err)
	}
	defer rc.Close()
	return e.writeFile(path, rc, modeFor(f.Mode()))
}

// entryPath 把归档里的条目名解析成目标目录内的绝对路径。
// skip=true 表示这条该整个跳过（macOS 元数据）。
func (e *extractor) entryPath(name string) (path string, skip bool, err error) {
	const op = "selfupdate.extract"
	e.count++
	if e.count > e.maxCount {
		return "", false, rejected(op, name, fmt.Sprintf("条目数超过 %d 上限", e.maxCount))
	}
	if name == "" {
		return "", false, rejected(op, name, "条目名为空")
	}
	// tar 与 zip 的规范都规定分隔符是 "/"。出现反斜杠只有两种可能：
	// 归档是坏的，或者有人在拿 Windows 路径试探非 Windows 上的解包器。
	if strings.Contains(name, `\`) {
		return "", false, rejected(op, name, "条目名含反斜杠")
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" ||
		strings.HasPrefix(clean, string(filepath.Separator)) {
		return "", false, rejected(op, name, "条目名是绝对路径")
	}
	if first, _, _ := strings.Cut(clean, string(filepath.Separator)); first == macOSMetaDir {
		return "", true, nil
	}
	full := filepath.Join(e.dst, clean)
	// 用 Rel 判越界而不是查 ".." 子串：子串法既挡不住所有花样，也会误伤
	// 名字里正常带 ".." 的文件。Rel 算的是「解析之后到底落在哪」。
	rel, err := filepath.Rel(e.dst, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, rejected(op, name, "条目路径越出了解包目录")
	}
	return full, false, nil
}

func (e *extractor) mkdir(path string) error {
	if err := os.MkdirAll(path, dirMode); err != nil {
		return errs.Wrap(errs.CategoryStorage, "selfupdate.extract", "解包时创建目录失败",
			"请确认磁盘空间充足且安装目录可写", err)
	}
	return nil
}

// writeFile 写一个条目，同时把它计进总量。O_EXCL 让「同名条目出现两次」变成错误
// —— 那是构造过的归档才会有的形状（第二条覆盖第一条，绕过针对第一条的检查）。
func (e *extractor) writeFile(path string, r io.Reader, mode fs.FileMode) (err error) {
	const op = "selfupdate.extract"
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "解包时创建目录失败",
			"请确认磁盘空间充足且安装目录可写", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "解包时写文件失败",
			"请确认磁盘空间充足且安装目录可写", err)
	}
	defer func() {
		cerr := f.Close()
		if err == nil && cerr != nil {
			err = errs.Wrap(errs.CategoryStorage, op, "解包时写文件失败", "请确认磁盘空间充足", cerr)
		}
	}()

	room := e.maxEntry + 1
	if left := e.maxTotal - e.total + 1; left < room {
		room = left
	}
	n, err := io.Copy(f, io.LimitReader(r, room))
	e.total += n
	if err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "解包时写文件失败",
			"请确认磁盘空间充足", err)
	}
	if n > e.maxEntry || e.total > e.maxTotal {
		return rejected(op, filepath.Base(path), "解包内容超过大小上限")
	}
	// OpenFile 的权限会被 umask 削掉（umask 077 时 0755 会变成 0700），
	// 而 .app 里的可执行文件必须对所有用户可执行，显式设回来。
	if err := os.Chmod(path, mode); err != nil {
		return errs.Wrap(errs.CategoryStorage, op, "解包时设置文件权限失败", "请重试更新", err)
	}
	return nil
}

// modeFor 只保留可执行位，其余一律归一化 —— 归档里的权限是构建机的产物，不该原样照搬。
func modeFor(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return execMode
	}
	return fileMode
}

func badArchive(op string, cause error) error {
	return errs.Wrap(errs.CategoryUpstream, op, "更新包已损坏，无法解开",
		"请重试更新；如果反复出现，请到项目发布页手动下载", cause)
}

// rejected 是「归档内容不符合安全约束」的统一出口。条目名来自归档，截断后再记。
func rejected(op, entry, why string) error {
	return errs.Wrap(errs.CategoryUpstream, op, "更新包内容不合法，已中止更新",
		"请到项目发布页手动下载；如果反复出现，说明下载来源可能被篡改",
		fmt.Errorf("%s：%s", why, trunc(entry, 80)))
}
