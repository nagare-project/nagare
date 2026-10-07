import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { AddFolderForm } from './AddFolderForm'
import { InstallGuide } from '../settings/MpvCard'
import { useSources } from '../../hooks/useSources'
import { useSourcePlugin } from '../../hooks/useSourcePlugin'
import type { SettingsState } from '../../hooks/useSettings'
import { redetectMpv } from '../../lib/endpoints'
import type { AddFolderData, MpvInstallGuide } from '../../lib/endpoints'
import { errorText } from '../../lib/format'

/**
 * 首次运行的引导屏。
 *
 * 取代原来那个「只有一个『交出文件夹』表单」的空态。换掉的理由是它把
 * 一件【可选】的事摆成了唯一入口：nagare 的两条播放路径互相独立，
 * 磁力搜索根本不需要任何本地文件夹，而旧空态从没告诉过新用户这件事。
 *
 * 形态上参照 seanime 的 getting-started —— 把每个准备项摊开、标明状态、
 * 允许跳过。但它是五步向导（它有五个配置区），nagare 只有四项，
 * 做成一屏总览比逼用户点五次好：向导的价值在于分解复杂度，
 * 四张卡片没有复杂度可分解，只剩点击成本。
 *
 * 唯一必需的是 mpv —— 没有它两条路都播不了。其余三项都明确标「可选」，
 * 并各自给出去处，用户随时可以什么都不配就去用另一条路。
 *
 * 两处最容易卡住新用户的地方就地解决：装 mpv（安装方法 + 重新检测都在卡片里，
 * 不跳设置页）与选文件夹（「浏览…」逐层点选，不用手敲绝对路径）。
 */

interface Props {
  settings: SettingsState
  onAdd: (path: string) => Promise<AddFolderData>
  /** 重新检测 mpv 之后刷新设置（useSettings.reload），就绪的绿点据此亮起 */
  onReloadSettings: () => Promise<void>
  /** 「先不添加本地文件夹」。只在 mpv 就绪时给出 —— mpv 是必需项，不能带着它跳过 */
  onSkipFolders?: () => void
}

/** 准备项的就绪状态：done 已完成 · todo 待办（非阻塞） · required 必需但缺失 */
type Readiness = 'done' | 'todo' | 'required'

export function GettingStarted({ settings, onAdd, onReloadSettings, onSkipFolders }: Props) {
  const sources = useSources()
  const plugin = useSourcePlugin()

  const mpv = settings.phase === 'ready' ? settings.data.mpv : null
  const loggedIn = settings.phase === 'ready' && settings.data.animego.loggedIn
  const rules = sources.state.phase === 'ready' ? sources.state.data : null
  // 「磁力可用」有两条来路：来源插件就绪且公布了启用的 BT 来源（安装包捆的那份首次运行
  // 就是这样），或者配了规则仓库且真的加载出了源。只看地址非空会把「填了地址但同步失败」
  // 说成已就绪，用户点进搜索页才发现搜不出东西。
  const pluginView = plugin.state.phase === 'ready' ? plugin.state.data : null
  const pluginBT = pluginView?.status?.phase === 'ready'
    ? (pluginView.sources ?? []).filter(source => source.kind === 'bt' && source.enabled).length
    : 0
  const ruleCount = rules !== null && rules.rules.remoteUrl !== '' ? rules.sources.length : 0
  const hasMagnetSource = pluginBT > 0 || ruleCount > 0
  // 有来源但磁力引擎没起来（端口被占等）时不能说「已就绪」：进了作品页才发现播不了
  const torrentOn = settings.phase !== 'ready' || settings.data.torrent.enabled
  const magnetReady = hasMagnetSource && torrentOn
  const platform = settings.phase === 'ready' ? settings.data.platform : undefined
  const magnetDesc = hasMagnetSource && !torrentOn
    ? '磁力引擎没有启动，暂时播放不了磁力。去设置里的「磁力播放」看原因。'
    : magnetReady
    ? `已就绪：${[pluginBT > 0 ? `内置来源插件 ${pluginBT} 个 BT 来源` : '', ruleCount > 0 ? `规则仓库 ${ruleCount} 个源` : ''].filter(Boolean).join('，')}，可以直接在作品页选集或搜索`
    : pluginView?.bundled
      ? '安装包内置了 Nagare Source（BT 来源），启用后即可搜索磁力。'
      : 'nagare 不内置任何搜索源。安装 Nagare Source 插件，或自己填一个规则仓库地址。'

  return (
    <section className="onboard" aria-labelledby="onboard-heading">
      <p className="onboard-mark" aria-hidden="true">
        流
      </p>
      <h2 id="onboard-heading" className="onboard-title">
        欢迎使用 nagare
      </h2>
      <p className="onboard-lede">
        本地文件和磁力链接，都交给你自己电脑上的 mpv 播放。
        <strong>两条路互相独立</strong>：不添加本地文件夹，也可以直接用磁力搜索。
      </p>

      <ul className="onboard-list">
        <Step
          state={mpv === null ? 'todo' : mpv.found ? 'done' : 'required'}
          name="播放器 mpv"
          tag="必需"
          desc={
            mpv?.found === true
              ? `已就绪${mpv.version !== undefined ? ` · ${mpv.version}` : ''}`
              : 'nagare 自己不解码，播放全部交给 mpv。没有它两条路都播不了。'
          }
        >
          {mpv !== null && !mpv.found && <MpvSetup hint={mpv.hint} install={mpv.install} onReload={onReloadSettings} />}
        </Step>

        <Step
          state="todo"
          name="本地文件夹"
          tag="可选"
          desc="选一个存放动漫的文件夹（外接硬盘也可以），nagare 会扫描并按作品整理。之后可以再加。"
        >
          <div className="onboard-form">
            <AddFolderForm onAdd={onAdd} autoFocus platform={platform} />
          </div>
        </Step>

        <Step
          state={magnetReady ? 'done' : 'todo'}
          name="磁力搜索"
          tag="可选"
          desc={magnetDesc}
        >
          {magnetReady ? (
            <Link to="/discover" className="btn btn--sm">去发现页选番</Link>
          ) : (
            <Link to="/settings" hash={hasMagnetSource ? 'torrent' : 'sources'} className="btn btn--sm">
              {hasMagnetSource ? '查看磁力播放设置' : '去设置来源'}
            </Link>
          )}
        </Step>

        <Step
          state={loggedIn ? 'done' : 'todo'}
          name="animego 账号"
          tag="可选"
          desc={
            loggedIn
              ? '已登录，弹幕与观看进度会自动同步'
              : '登录后才有弹幕，观看进度也会同步回账号。不登录不影响播放。'
          }
        >
          {!loggedIn && (
            <Link to="/settings" hash="account" className="btn btn--sm">
              去登录
            </Link>
          )}
        </Step>
      </ul>

      {onSkipFolders !== undefined && (
        <p className="onboard-skip">
          只用磁力看番？
          <button type="button" className="link onboard-skip-btn" onClick={onSkipFolders}>
            先不添加本地文件夹
          </button>
        </p>
      )}
    </section>
  )
}

/**
 * mpv 缺失时直接在引导卡里给出安装方法与「重新检测」，不再跳去设置页 ——
 * 跳走之后新用户要自己找回首页，这一步最容易半途而废。
 *
 * 装 mpv 的人多半是切到终端敲完命令再切回来，所以页面重新可见 / 窗口重新获得焦点时
 * 自动检测一次；检测到了设置随之刷新，这张卡片变成绿点「已就绪」。
 * 自动检测是静默的：不禁用按钮、不出失败提示；用户此时点了按钮，就在这次结束后告诉他结果。
 */
function MpvSetup({ hint, install, onReload }: { hint?: string; install?: MpvInstallGuide; onReload: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const running = useRef(false)
  const manualWaiting = useRef(false)

  const detect = useCallback(
    async (manual: boolean) => {
      if (manual) setMessage(null) // 清掉再写：同一句失败提示连写两次，读屏不会再念
      if (running.current) {
        if (manual) manualWaiting.current = true
        return
      }
      running.current = true
      if (manual) setBusy(true)
      try {
        const info = await redetectMpv()
        const report = manual || manualWaiting.current
        manualWaiting.current = false
        // 只在找到时刷新设置：没变化就不碰它，刷新失败会让整张引导屏退回加载失败
        if (info.found) await onReload()
        else if (report) setMessage(failureReason(info.hint) ?? '还是没找到 mpv。确认安装命令已经跑完，再点一次「重新检测」。')
      } catch (err) {
        console.error('重新检测 mpv 失败', err)
        if (manual || manualWaiting.current) setMessage(errorText(err, '重新检测失败'))
        manualWaiting.current = false
      } finally {
        running.current = false
        setBusy(false)
      }
    },
    [onReload],
  )

  useEffect(() => {
    function onReturn(): void {
      if (document.visibilityState === 'visible') void detect(false)
    }
    window.addEventListener('focus', onReturn)
    document.addEventListener('visibilitychange', onReturn)
    return () => {
      window.removeEventListener('focus', onReturn)
      document.removeEventListener('visibilitychange', onReturn)
    }
  }, [detect])

  const reason = failureReason(hint)
  return (
    <div className="onboard-mpv">
      {/* 「找到了但用不了」（版本过低、无法执行）光看安装命令是看不出来的，原因必须写在这里 */}
      {reason !== null && (
        <p className="alert-warn" role="alert">
          {reason}
        </p>
      )}
      {install !== undefined && <InstallGuide install={install} />}
      <div className="form-actions">
        <button type="button" className="btn btn--sm btn--primary" onClick={() => void detect(true)} disabled={busy}>
          {busy ? '检测中 …' : '我装好了，重新检测'}
        </button>
      </div>
      <p className={message === null ? 'result result--dim' : 'result result--err'} role="status" aria-live="polite">
        {message ?? '装好后切回这个页面会自动检测，不用重启 nagare。'}
      </p>
    </div>
  )
}

/**
 * 后端提示里值得单独亮出来的原因。「未找到 mpv（已检查…）：请先安装…」与下面的安装方法是同一件事，
 * 不再重复；版本过低、找到了却无法执行这类，安装命令解释不了，必须原样给出。
 * （按后端 internal/mpv/detect.go 的措辞前缀区分。）
 */
function failureReason(hint: string | undefined): string | null {
  if (hint === undefined || hint === '' || hint.startsWith('未找到 mpv')) return null
  return hint
}

interface StepProps {
  state: Readiness
  name: string
  tag: '必需' | '可选'
  desc: string
  children?: React.ReactNode
}

function Step({ state, name, tag, desc, children }: StepProps) {
  return (
    <li className="onboard-step">
      <span className={`onboard-dot onboard-dot--${state}`} aria-hidden="true" />
      <div className="onboard-body">
        <p className="onboard-step-head">
          <span className="onboard-step-name">{name}</span>
          {/* 「必需」只在【尚未满足】时才染警示色：已就绪的绿点配琥珀徽标
              是自相矛盾的信号，用户会以为还有事要做 */}
          <span className={state === 'required' ? 'badge badge--warn' : 'badge'}>{tag}</span>
        </p>
        <p className="onboard-step-desc">{desc}</p>
        {children}
      </div>
    </li>
  )
}
