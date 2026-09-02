/**
 * 写剪贴板（搜索页「复制磁力」与设置页「复制安装命令」共用）。
 *
 * 优先 navigator.clipboard.writeText —— 需要安全上下文，127.0.0.1 / localhost 算安全；
 * 不可用或被拒（权限 / 非用户手势）时回退到「临时 textarea + document.execCommand('copy')」，
 * 后者虽已废弃但各浏览器仍保留。返回是否成功；失败原因留在控制台，界面只需展示「复制失败」。
 */
export async function copyText(text: string): Promise<boolean> {
  const clipboard = navigator.clipboard as Clipboard | undefined
  if (clipboard !== undefined) {
    try {
      await clipboard.writeText(text)
      return true
    } catch (err) {
      console.error('navigator.clipboard 写入失败，改用 execCommand 回退', err)
    }
  }
  return copyViaExecCommand(text)
}

/** 旧式回退：把文本放进屏幕外的 textarea 选中后执行 copy 命令 */
function copyViaExecCommand(text: string): boolean {
  // jsdom 与部分嵌入式 WebView 没有 execCommand，先探测再用
  const exec = (document as Document & { execCommand?: (command: string) => boolean }).execCommand
  if (typeof exec !== 'function') {
    console.error('复制失败：当前环境既没有 navigator.clipboard 也没有 document.execCommand')
    return false
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '-9999px'
  document.body.appendChild(textarea)
  try {
    textarea.select()
    const ok = exec.call(document, 'copy')
    if (!ok) console.error('复制失败：document.execCommand("copy") 返回 false')
    return ok
  } catch (err) {
    console.error('复制失败：document.execCommand("copy") 抛出异常', err)
    return false
  } finally {
    textarea.remove()
  }
}
