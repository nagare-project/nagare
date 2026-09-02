/**
 * 测试用的内存 localStorage。
 * Node ≥ 22 自带一个全局 `localStorage`（不带 --localstorage-file 时只是个空壳），
 * vitest 的 jsdom 环境不会用 jsdom 的实现覆盖它，于是 `window.localStorage.getItem`
 * 在测试里不是函数。浏览器不受影响；测试里用这个替身。
 */
export function memoryStorage(): Storage {
  const map = new Map<string, string>()
  return {
    get length() {
      return map.size
    },
    clear: () => map.clear(),
    getItem: (key) => map.get(key) ?? null,
    key: (index) => Array.from(map.keys())[index] ?? null,
    removeItem: (key) => {
      map.delete(key)
    },
    setItem: (key, value) => {
      map.set(key, String(value))
    },
  }
}

/** 把 window.localStorage 换成给定实现（默认全新的内存版）；每个用例 beforeEach 调一次即可隔离 */
export function installLocalStorage(storage: Storage = memoryStorage()): Storage {
  Object.defineProperty(window, 'localStorage', { value: storage, configurable: true, writable: true })
  return storage
}
