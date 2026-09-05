import './styles/global.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from '@tanstack/react-router'
import { acquireToken } from './lib/token'
import { router } from './routes'

// 先于渲染消化启动链接里的 ?token=<hex>：
// 同源持久保存并把它从地址栏抹掉，路由挂载后看到的就是干净的 URL。
acquireToken()

const rootElement = document.getElementById('root')
if (rootElement === null) {
  throw new Error('index.html 中找不到 #root 挂载点')
}

createRoot(rootElement).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
