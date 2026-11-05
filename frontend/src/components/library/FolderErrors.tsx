import { Link } from '@tanstack/react-router'
import type { LibraryFolder } from '../../lib/endpoints'

/**
 * 上次扫描读不到的文件夹（DESIGN.md「状态表 · 媒体库 · 出错」：扫描出错时用横幅说明原因）。
 * 文件夹失效（拔掉的移动硬盘、改了名的目录）时，里面的番不会更新，也不会从媒体库里消失 ——
 * 不说出来，用户只会觉得「新下的集怎么没出来」。原因是后端给的原话，恢复动作在设置页的文件夹管理里。
 */
export function FolderErrors({ folders }: { folders: LibraryFolder[] }) {
  const broken = folders.filter((folder) => folder.error !== undefined && folder.error !== '')
  if (broken.length === 0) return null
  const lead = broken.length === 1 ? '有 1 个文件夹这次没能扫描，里面的番不会更新：' : `有 ${broken.length} 个文件夹这次没能扫描，里面的番不会更新：`
  return (
    <div className="alert-warn lib-folder-errors" role="status">
      <p>{lead}</p>
      <ul className="lib-folder-errors-list">
        {broken.map((folder) => (
          <li key={folder.id}>
            <span className="lib-folder-errors-path">{folder.path}</span>
            <span className="lib-folder-errors-why">{folder.error}</span>
          </li>
        ))}
      </ul>
      <Link to="/settings" hash="folders" className="link alert-warn-link">检查文件夹的路径与权限 →</Link>
    </div>
  )
}
