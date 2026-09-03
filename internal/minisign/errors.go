package minisign

import (
	"errors"

	errs "github.com/nagare-project/nagare/internal/errors"
)

// 下面这组哨兵错误让调用方能用 errors.Is 精确分辨失败原因。
// 它们全部被包在 errs.E 里（errs.E 实现了 Unwrap），所以同一个 error 既带
// 给界面看的中文 UserMsg + 恢复动作，也带一个可判定的身份给代码。
//
// 为什么要分这么细：这几种失败指向完全不同的原因，揉成一句「校验失败」会让排查
// 从「看一眼日志」变成「猜」——
//   - key id 不匹配   → 发布流程用错了私钥，或者用户下的根本不是本项目的包
//   - 第一段签名不匹配 → 归档内容被改过或下载损坏
//   - 全局签名不匹配   → 文件本身没问题，但 trusted comment（版本号/文件名）被改过
//   - 格式错          → 下到的是一个 HTML 错误页 / 半截文件，压根不是签名
var (
	// ErrTooLarge：输入超过 maxInputSize。
	ErrTooLarge = errors.New("minisign: input exceeds size limit")
	// ErrPublicKeyFormat：公钥文本不是合法的 minisign 公钥。
	ErrPublicKeyFormat = errors.New("minisign: malformed public key")
	// ErrSignatureFormat：签名文本不是合法的 .minisig。
	ErrSignatureFormat = errors.New("minisign: malformed signature file")
	// ErrUnsupportedAlgorithm：算法字段既不是 "Ed" 也不是 "ED"。
	ErrUnsupportedAlgorithm = errors.New("minisign: unsupported signature algorithm")
	// ErrKeyIDMismatch：签名的 key id 与公钥的 key id 不一致 —— 这个签名不是这把公钥签的。
	ErrKeyIDMismatch = errors.New("minisign: key id mismatch")
	// ErrSignatureMismatch：第一段签名与消息不匹配 —— 内容被改过。
	ErrSignatureMismatch = errors.New("minisign: signature does not match content")
	// ErrGlobalSignature：第二段（全局）签名不匹配 —— trusted comment 被改过。
	ErrGlobalSignature = errors.New("minisign: trusted comment signature does not match")
)

// 恢复动作文案。用户能做的事只有这么几种，集中放一处免得各处写岔。
const (
	recoveryRedownload = "请重新下载后重试；如果反复失败，请到项目仓库反馈"
	recoveryUpgrade    = "请升级到最新版 nagare；若已是最新版，请到项目仓库反馈"
	recoverySource     = "请确认下载来源是本项目的官方 Releases 页"
	recoveryBuild      = "这是 nagare 自身的构建缺陷，请到项目仓库反馈"
)

// keyErr 归 errs.CategoryInternal：公钥是编译进二进制的常量，它不合法说明
// 构建时就配错了，跟用户的网络、跟下载到的文件都没关系，重试多少次都一样。
func keyErr(op, userMsg string, cause error) *errs.E {
	return errs.Wrap(errs.CategoryInternal, op, userMsg, recoveryBuild, cause)
}

// sigErr 归 errs.CategoryUpstream：签名文件是下载来的，它不对说明「下载到的东西不对」。
func sigErr(op, userMsg, recovery string, cause error) *errs.E {
	return errs.Wrap(errs.CategoryUpstream, op, userMsg, recovery, cause)
}
