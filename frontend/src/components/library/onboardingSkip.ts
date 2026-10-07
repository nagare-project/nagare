/** 「先不添加本地文件夹」记在 localStorage 里的键；值固定为 '1' */
export const SKIP_FOLDERS_KEY = 'nagare.onboarding.skipFolders'

/**
 * 只用磁力的人不需要本地文件夹，没理由每次打开首页都看一整屏「欢迎使用」。
 * 跳过只是本机浏览器里的一个记号：localStorage 在隐私模式 / 禁用站点数据时会抛，
 * 读写都包起来，读不到就当没跳过（多看一次引导，不会少功能）。
 */
export function readSkipFolders(): boolean {
  try {
    return window.localStorage.getItem(SKIP_FOLDERS_KEY) === '1'
  } catch (err) {
    console.error('读取「跳过本地文件夹」失败', err)
    return false
  }
}

export function writeSkipFolders(skip: boolean): void {
  try {
    if (skip) window.localStorage.setItem(SKIP_FOLDERS_KEY, '1')
    else window.localStorage.removeItem(SKIP_FOLDERS_KEY)
  } catch (err) {
    console.error('记录「跳过本地文件夹」失败', err)
  }
}
