// 合集里换集：同一个种子里的另一个视频文件，在同一个 mpv 窗口里接着播（窗口那一侧见 internal/player/switch.go）。
//
// 邻居按 internal/episodenav 挑，与本地媒体库是同一条规则：只在认出集号的正片之间走，必须是同一部作品、
// 同一季 —— 一个合集里混着两部番时不会从这一部的第 3 集「下一集」到另一部的第 4 集。
//
// 换文件握着 prepMu 做，与 Prepare 互斥。网页上再点一次播放时，Prepare 在抢 prepMu 之前先经 interrupt
// 收掉这个会话，换集随即以「种子已经停了」返回、交还 prepMu，不会把 Prepare 晾在锁上等满缓冲上限。
//
// 换集期间会话一直是活的（Status.Active 为真、phase 是 buffering），网页上的状态条不会收摊；
// 被换走的那一集照常能服务（retiredFile），只是不再有优先级窗口。换集失败时回到换集之前的那一集：
// 它的窗口按 mpv 的最新读位置重画（priorityManager.reopen），而不是从起播状态重来。
package torrentstream

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/nagare-project/nagare/internal/episodenav"
	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/library"
)

const opSwitch = "torrentstream.switch"

// retiredFile 是合集里刚被换走的那一集：到下一次换文件或会话结束为止照常服务（见 session.target）。
//
// 换集准备期间旧的一集还在播，这时拖一下进度条 mpv 会带着旧地址重新请求；回 404 会让 mpv 直接结束
// 这一集，用户看到的是「点了下一集，窗口没了」。
type retiredFile struct {
	index int
	file  *torrent.File
	name  string
}

// fileState 是换集之前那一集的全部状态，换集失败时照原样换回去（见 session.restore）。
type fileState struct {
	file    *torrent.File
	index   int
	item    library.Item
	pm      *priorityManager
	retired *retiredFile
	phase   string
	prio    torrent.PiecePriority
}

// torrentFileID 是种子里一个文件的 FileID：同一条磁力的同一个文件永远是同一个 id，跨会话可续播。
func torrentFileID(infohash string, index int) string {
	return "t:" + infohash + "/" + strconv.Itoa(index)
}

// Siblings 返回这一集在同一个种子里的上一集与下一集（没有的一侧为 nil）。
// 会话已经结束、这一集已经不能服务、单集种子、这一集认不出集号时两边都是 nil。每集开播时调，只读内存。
func (s *Source) Siblings() (prev, next *FileChoice) {
	if s.sess == nil {
		return nil, nil
	}
	return s.sess.siblingChoice(s.infohash, s.index, -1), s.sess.siblingChoice(s.infohash, s.index, 1)
}

// OpenSibling 把同一个种子里 dir 方向（-1 上一集、1 下一集）的那一集缓冲到能起播，返回它的 Source。
// 目录作品身份（MatchHints）照抄这一集的：用户是在同一部作品里接着看。
//
// 失败时回到这一集（会话还活着的话），错误都已分类、带中文说法与恢复动作。ctx 取消时尽快返回。
func (s *Source) OpenSibling(ctx context.Context, dir int) (*Source, error) {
	if dir == 0 {
		return nil, errs.New(errs.CategoryInternal, opSwitch, "切集方向无效", "这是 nagare 自身的问题，请反馈")
	}
	if s.sess == nil {
		return nil, errSwitchStopped(dir, nil)
	}
	next, err := s.sess.engine.switchFile(ctx, s, dir)
	if err != nil {
		return nil, err
	}
	next.anilistID = s.anilistID
	next.titles = append([]string(nil), s.titles...)
	return next, nil
}

// Discard 在窗口最终没有换上这一集时调用（换集被取消了、mpv 没切过去）：把种子换回换集之前的那一集，
// 它的优先级窗口按 mpv 的最新读位置重画 —— 不然窗口还在播旧的一集，种子却在下新的一集。
// 只对 OpenSibling 交出的 Source 有效；之后又换过文件（再切了一次、网页上重新播了）时什么都不做。
// 要拿 prepMu，可能阻塞，调用方放到后台。
func (s *Source) Discard() {
	if s.discard != nil {
		s.discard()
	}
}

// switchFile 在 from 所在的会话里换到 dir 方向的那一集。
func (e *Engine) switchFile(ctx context.Context, from *Source, dir int) (*Source, error) {
	e.prepMu.Lock()
	defer e.prepMu.Unlock()

	sess := from.sess
	if !e.isCurrent(sess) {
		return nil, errSwitchStopped(dir, nil)
	}
	tor := sess.currentTorrent()
	if tor == nil || tor.Info() == nil {
		return nil, errSwitchStopped(dir, nil)
	}
	// 重新挑一次：开播时报给界面的邻居之后，会话可能已经换过文件
	target, ok := sess.sibling(from.infohash, from.index, dir)
	if !ok {
		return nil, errs.New(errs.CategoryTorrent, opSwitch,
			"这个种子里没有"+switchWord(dir), "回到网页上选别的资源")
	}

	before, ok := sess.snapshotFile()
	if !ok {
		return nil, errSwitchStopped(dir, nil)
	}
	// 要切去的那一集已经是活的（上一次换过去了、窗口却没跟上，还停在旧的一集）：不用重来，等它缓冲好就行
	if before.index != target.index {
		if err := sess.startFile(tor, selection{Index: target.index, Item: target.item}); err != nil {
			sess.restore(before)
			if sess.isClosed() {
				return nil, errSwitchStopped(dir, err)
			}
			return nil, errs.Wrap(errs.CategoryTorrent, opSwitch,
				"种子里的"+switchWord(dir)+"没能打开", "回到网页上选这一集播放", err)
		}
		sess.keepServable(from.index, before)
	}
	log.Printf("torrent: 合集里换到%s，开始缓冲", switchWord(dir))
	sess.setPhase(PhaseBuffering)
	started := time.Now()
	readBefore := usefulBytesRead(tor)
	if err := sess.waitBuffered(ctx); err != nil {
		sess.restore(before)
		return nil, switchFailure(ctx, sess, dir, err)
	}
	sess.openGate()
	sess.setPhase(PhaseReady)
	sess.logBuffered(tor, time.Since(started), usefulBytesRead(tor)-readBefore)
	out := sess.newSource()
	gen := sess.bumpFileGen()
	out.discard = func() { e.revertSwitch(sess, gen, before) }
	return out, nil
}

// revertSwitch 把一次窗口没有跟上的换集换回去（Source.Discard）。会话已经不是当前会话、
// 之后又换过文件（fileGen 变了）时什么都不做：迟到的「换回去」不能把后来的换集撤掉。
func (e *Engine) revertSwitch(sess *session, gen uint64, before fileState) {
	e.prepMu.Lock()
	defer e.prepMu.Unlock()
	if !e.isCurrent(sess) || !sess.fileGenIs(gen) {
		return
	}
	sess.restore(before)
	sess.bumpFileGen()
	log.Printf("torrent: 窗口没有换到准备好的那一集，种子换回原来那一集")
}

// bumpFileGen 记一次换文件，返回新的代次。
func (s *session) bumpFileGen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileGen++
	return s.fileGen
}

func (s *session) fileGenIs(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fileGen == gen
}

// isCurrent 报告 sess 是否仍是引擎当前的会话。
func (e *Engine) isCurrent(sess *session) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.closed && e.sess == sess
}

// snapshotFile 记下换集之前那一集的状态；会话已经结束或还没选过文件时 ok 为 false。
func (s *session) snapshotFile() (fileState, bool) {
	s.mu.Lock()
	st := fileState{file: s.file, index: s.index, item: s.item, pm: s.pm, retired: s.retired, phase: s.phase}
	ok := !s.closed && s.file != nil
	s.mu.Unlock()
	if !ok {
		return fileState{}, false
	}
	// 文件这一级的优先级要在锁外读：它拿 client 锁，而 anacrolix 的回调（写盘失败）会在 client 锁里来拿 s.mu
	st.prio = st.file.Priority()
	return st, true
}

// keepServable 让窗口里正在播的那一集（from）在换集期间继续能服务。
//
// startFile 留着的是换集之前活着的那一集。通常就是 from；但上一次换过去之后窗口没跟上（mpv 没切成，
// 还在播被换走的那一集）时，活着的是另一集，from 是被换走的那一集 —— 那时要留的是 from。
func (s *session) keepServable(fromIndex int, before fileState) {
	if before.index == fromIndex || before.retired == nil || before.retired.index != fromIndex {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.retired = before.retired
	}
}

// restore 换集没成：回到换集之前的那一集。会话已经结束、文件根本没换过时什么都不做。
//
// 顺序与 startFile 一致：先停用失败那一集的窗口、再取消它，然后按 mpv 的最新读位置重画旧的窗口，
// 最后才把旧文件的其余部分放开（窗口先钉上，见 session.openGate）。
func (s *session) restore(before fileState) {
	s.mu.Lock()
	if s.closed || before.file == nil {
		s.mu.Unlock()
		return
	}
	s.phase = before.phase // 没换文件（要切去的那一集本来就是活的）也要把「缓冲中」收回来
	if s.file == before.file {
		s.mu.Unlock()
		return
	}
	failed, failedPM := s.file, s.pm
	s.file, s.index, s.item, s.pm = before.file, before.index, before.item, before.pm
	s.retired = before.retired
	s.mu.Unlock()

	if failedPM != nil {
		failedPM.close()
	}
	if failed != nil {
		failed.Cancel()
	}
	if before.pm != nil {
		before.pm.reopen()
	}
	before.file.SetPriority(before.prio)
	log.Printf("torrent: 换集没成，回到原来那一集")
}

// isClosed 报告会话是否已经收掉（close 先置位、后取消 ctx，两者之间只看得到这一个）。
func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// setCandidates 记下这个种子里可播放的候选（选集时算的那一份）。
func (s *session) setCandidates(cands []candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cands = cands
}

// servableLocked 报告 infohash + index 是否还能服务（当前这一集，或刚被换走的那一集）。调用方持 s.mu。
func (s *session) servableLocked(infohash string, index int) bool {
	if s.closed || s.infohash == "" || infohash != s.infohash {
		return false
	}
	return (s.file != nil && s.index == index) || (s.retired != nil && s.retired.index == index)
}

// sibling 在这个种子里为第 index 个文件挑 dir 方向的邻居集；它已经不能服务时没有。
func (s *session) sibling(infohash string, index, dir int) (candidate, bool) {
	s.mu.Lock()
	if !s.servableLocked(infohash, index) {
		s.mu.Unlock()
		return candidate{}, false
	}
	cands := s.cands
	s.mu.Unlock()
	return siblingOf(cands, infohash, index, dir)
}

// siblingChoice 是 sibling 交给界面的形状（没有为 nil）。
func (s *session) siblingChoice(infohash string, index, dir int) *FileChoice {
	c, ok := s.sibling(infohash, index, dir)
	if !ok {
		return nil
	}
	choice := toChoices([]candidate{c})[0]
	return &choice
}

// siblingOf 在候选 cands 里为种子里第 index 个文件挑 dir 方向的邻居集（规则见 episodenav.Pick）。
//
// 当前这一集按种子内下标认；候选的 FileID 换成种子内的稳定 id（torrentFileID）再交给 Pick ——
// 解析链给的软 id 是「文件名|大小|0」，合集里两个子目录下的同名同大小文件会撞成同一个，
// Pick 按 FileID 跳过当前这一集时就分不清是哪一份。
func siblingOf(cands []candidate, infohash string, index, dir int) (candidate, bool) {
	items := make([]library.Item, len(cands))
	cur := -1
	for i, c := range cands {
		items[i] = c.item
		items[i].FileID = torrentFileID(infohash, c.index)
		if c.index == index {
			cur = i
		}
	}
	if cur < 0 {
		return candidate{}, false
	}
	i, ok := episodenav.Pick(items[cur], items, dir)
	if !ok {
		return candidate{}, false
	}
	return cands[i], true
}

// switchFailure 把换集途中的失败翻成界面上的说法（屏幕提示与错误卡片直接用 UserMsg / Recovery）。
func switchFailure(ctx context.Context, sess *session, dir int, cause error) error {
	switch {
	case ctx.Err() != nil:
		return errs.Wrap(errs.CategoryTorrent, opSwitch, "切换"+switchWord(dir)+"已取消", "", cause)
	case sess.ctx.Err() != nil || sess.isClosed():
		return errSwitchStopped(dir, cause)
	}
	var ce *errs.E
	if errors.As(cause, &ce) && ce.Category == errs.CategoryStorage {
		// 磁盘写不进去：原样交出它自己的说法与恢复动作（腾空间、换目录，与分享者多少无关）
		return cause
	}
	return errs.Wrap(errs.CategoryTorrent, opSwitch,
		switchWord(dir)+"缓冲太慢，分享者太少", "可以重试，或者回到网页上换一条分享者更多的资源", cause)
}

// errSwitchStopped 是换集时会话已经没了（网页上停了播放、换了另一条磁力、引擎关了）。
func errSwitchStopped(dir int, cause error) error {
	return errs.Wrap(errs.CategoryTorrent, opSwitch,
		"种子已经停了，没法切到"+switchWord(dir), "回到网页上重新播放这一集", cause)
}

func switchWord(dir int) string {
	if dir < 0 {
		return "上一集"
	}
	return "下一集"
}
