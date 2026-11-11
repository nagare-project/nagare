/**
 * jsdom 没有 IntersectionObserver：换一个可以手动触发的替身。
 * intersect(element) 让所有观察着这个元素的观察器报一次「进入范围」。
 */
interface FakeObserver {
  callback: IntersectionObserverCallback
  options: IntersectionObserverInit | undefined
  targets: Set<Element>
}

export interface FakeIntersectionObservers {
  observers: FakeObserver[]
  intersect: (element: Element) => void
  isObserved: (element: Element) => boolean
  restore: () => void
}

export function installFakeIntersectionObserver(): FakeIntersectionObservers {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'IntersectionObserver')
  const observers: FakeObserver[] = []
  class FakeIntersectionObserver {
    private readonly record: FakeObserver
    constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
      this.record = { callback, options, targets: new Set() }
      observers.push(this.record)
    }
    observe(target: Element) { this.record.targets.add(target) }
    unobserve(target: Element) { this.record.targets.delete(target) }
    disconnect() { this.record.targets.clear() }
    takeRecords(): IntersectionObserverEntry[] { return [] }
  }
  Object.defineProperty(globalThis, 'IntersectionObserver', { configurable: true, writable: true, value: FakeIntersectionObserver })
  return {
    observers,
    intersect(element) {
      for (const record of observers) {
        if (!record.targets.has(element)) continue
        const entry = { target: element, isIntersecting: true } as IntersectionObserverEntry
        record.callback([entry], {} as IntersectionObserver)
      }
    },
    isObserved: (element) => observers.some((record) => record.targets.has(element)),
    restore() {
      if (original) Object.defineProperty(globalThis, 'IntersectionObserver', original)
      else Reflect.deleteProperty(globalThis, 'IntersectionObserver')
    },
  }
}
