import { Link } from '@tanstack/react-router'
import { AddFolderForm } from './AddFolderForm'
import { useSources } from '../../hooks/useSources'
import type { SettingsState } from '../../hooks/useSettings'
import type { AddFolderData } from '../../lib/endpoints'

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
 */

interface Props {
  settings: SettingsState
  onAdd: (path: string) => Promise<AddFolderData>
}

/** 准备项的就绪状态：done 已完成 · todo 待办（非阻塞） · required 必需但缺失 */
type Readiness = 'done' | 'todo' | 'required'

export function GettingStarted({ settings, onAdd }: Props) {
  const sources = useSources()

  const mpv = settings.phase === 'ready' ? settings.data.mpv : null
  const loggedIn = settings.phase === 'ready' && settings.data.animego.loggedIn
  const rules = sources.state.phase === 'ready' ? sources.state.data : null
  // 「磁力可用」= 配了规则仓库且真的加载出了源。只看地址非空会把
  // 「填了地址但同步失败」说成已就绪，用户点进搜索页才发现搜不出东西。
  const magnetReady = rules !== null && rules.rules.remoteUrl !== '' && rules.sources.length > 0

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
          {mpv !== null && !mpv.found && (
            <Link to="/settings" className="btn btn--sm btn--primary">
              去安装
            </Link>
          )}
        </Step>

        <Step
          state="todo"
          name="本地文件夹"
          tag="可选"
          desc="填入存放动漫的文件夹绝对路径，扫描后即可播放。之后可以再加。"
        >
          <div className="onboard-form">
            <AddFolderForm onAdd={onAdd} autoFocus />
          </div>
        </Step>

        <Step
          state={magnetReady ? 'done' : 'todo'}
          name="磁力搜索"
          tag="可选"
          desc={
            magnetReady
              ? `已加载 ${rules?.sources.length ?? 0} 个源，可以直接搜索`
              : 'nagare 不内置任何搜索源。要用磁力搜索，得先自己填一个规则仓库地址。'
          }
        >
          <Link to={magnetReady ? '/search' : '/settings'} className="btn btn--sm">
            {magnetReady ? '去搜索' : '配置规则仓库'}
          </Link>
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
            <Link to="/settings" className="btn btn--sm">
              去登录
            </Link>
          )}
        </Step>
      </ul>
    </section>
  )
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
