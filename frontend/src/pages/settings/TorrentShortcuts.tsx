import { Link } from '@tanstack/react-router'
import { Icon } from '../../components/ui/Icon'

/** 磁力任务与磁力搜索不在顶栏里（顶栏只在有任务时出现指示），从这里随时能到 */
export function TorrentShortcuts() {
  return (
    <nav className="info-panel" aria-label="磁力任务与搜索">
      <ul className="settings-shortcuts">
        <li>
          <Link to="/torrents" className="settings-shortcut">
            <Icon name="download" size={20} />
            <span className="settings-shortcut-text">
              磁力任务
              <small>边下边播的会话与整部下载的进度</small>
            </span>
            <Icon name="right" size={18} />
          </Link>
        </li>
        <li>
          <Link to="/search" className="settings-shortcut">
            <Icon name="search" size={20} />
            <span className="settings-shortcut-text">
              搜索磁力资源
              <small>按关键词在已启用的来源里搜</small>
            </span>
            <Icon name="right" size={18} />
          </Link>
        </li>
      </ul>
    </nav>
  )
}
