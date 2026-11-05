/**
 * API 黄金响应（仓库根 testdata/api/*.json）的严格解码器 —— 只给测试用，不进产物。
 *
 * 每个 schema 同时是两样东西：
 * - 运行时：逐字段检查一份 JSON。缺必填字段、类型不对、多出不认识的字段，都算错。
 * - 编译期：`exact<LibraryItem>()(obj(...))` 要求 obj 推出来的类型与 LibraryItem【完全一致】
 *   （逐层比较：字段一个不多、一个不少、可选性与取值范围都一样），否则 `bun run typecheck` 报错 ——
 *   所以 schema 不会和 endpoints.ts 里的类型各说各话。只用「互相可赋值」查不出漏掉的可选字段。
 *
 * 另外记下每个对象节点的可选字段在黄金文件里出现过没有、每个联合类型的分支命中过没有：
 * 一个可选字段从没出现过，要么是黄金用例不够全，要么是 TS 里留着一个后端早就不发的字段。
 *
 * 没用 zod 一类的库：几十行就够，而且不给前端多一个运行时依赖。
 */

export interface Schema<T> {
  readonly check: (value: unknown, path: string, ctx: CheckContext) => void
  /** 只给类型系统看：参数与返回值同时出现 T，Schema<A> 与 Schema<B> 只在 A、B 互相可赋值时兼容 */
  readonly __type?: (value: T) => T
}

export type Infer<S> = S extends Schema<infer T> ? T : never

/** 两个类型完全一致（不是互相可赋值：`{ a: 1 }` 与 `{ a: 1; b?: 2 }` 互相可赋值，但不一致） */
export type Equal<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false

/** 约束里只能用 any：Schema 是不变的，Schema<string> 不是 Schema<unknown> */
export type AnySchema = Schema<any>

/** 只用来检查一份数据的 schema（不关心它推出的类型）：测试里按名字登记一张表时用 */
export type Contract = Pick<Schema<unknown>, 'check'>
type Shape = Record<string, AnySchema>
type Simplify<T> = { [K in keyof T]: T[K] } & {}
type ObjType<R extends Shape, O extends Shape> = Simplify<
  { [K in keyof R]: Infer<R[K]> } & { [K in keyof O]?: Infer<O[K]> }
>

interface ObjectNode {
  readonly optional: readonly string[]
}

/**
 * 所有带可选字段的对象节点（构造时登记）。只看「检查时走到过的节点」的话，挂在一个永远为空的数组下、
 * 或者挂在从没出现过的可选字段下的对象，连同它的可选字段一起漏查 —— 所以没走到过的节点本身也算没覆盖。
 */
const objectsWithOptional: ObjectNode[] = []

/** 为假时新建的对象节点不进上面的登记表（见 withoutCoverage） */
let registerForCoverage = true

/**
 * 在 build 里建出来的 schema 不参与覆盖检查：给还没有黄金文件的接口用（test/smokeContracts.ts）。
 * 不这样做的话，它们的可选字段一导入就登记，黄金文件的覆盖检查会因为「从没被检查到」而失败 ——
 * 是否失败还取决于两份测试文件是不是共用一份模块。
 */
export function withoutCoverage<T>(build: () => T): T {
  const previous = registerForCoverage
  registerForCoverage = false
  try {
    return build()
  } finally {
    registerForCoverage = previous
  }
}

interface UnionNode {
  readonly size: number
}

/** 一轮检查的状态：错误、可选字段 / 联合分支的覆盖情况 */
export class CheckContext {
  readonly errors: string[] = []
  readonly objects = new Map<ObjectNode, { path: string; seen: Set<string> }>()
  readonly unions = new Map<UnionNode, { path: string; seen: Set<number> }>()

  fail(path: string, message: string): void {
    this.errors.push(`${path}: ${message}`)
  }

  /** 试探一个联合分支用的子上下文：分支没对上时它记下的覆盖信息要丢掉 */
  child(): CheckContext {
    return new CheckContext()
  }

  merge(other: CheckContext): void {
    this.errors.push(...other.errors)
    for (const [node, info] of other.objects) {
      const mine = this.objects.get(node)
      if (mine === undefined) this.objects.set(node, { path: info.path, seen: new Set(info.seen) })
      else info.seen.forEach(key => mine.seen.add(key))
    }
    for (const [node, info] of other.unions) {
      const mine = this.unions.get(node)
      if (mine === undefined) this.unions.set(node, { path: info.path, seen: new Set(info.seen) })
      else info.seen.forEach(i => mine.seen.add(i))
    }
  }

  /** 检查过的对象里从没出现过的可选字段、从没命中过的联合分支 */
  uncovered(): string[] {
    const out: string[] = []
    for (const node of objectsWithOptional) {
      if (!this.objects.has(node)) out.push(`（从没被检查到的对象，可选字段：${node.optional.join('、')}）`)
    }
    for (const [node, info] of this.objects) {
      for (const key of node.optional) if (!info.seen.has(key)) out.push(`${info.path}.${key}`)
    }
    for (const [node, info] of this.unions) {
      for (let i = 0; i < node.size; i++) if (!info.seen.has(i)) out.push(`${info.path}（联合类型第 ${i + 1} 个分支）`)
    }
    return out
  }
}

function describe(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return '数组'
  return typeof value
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function primitive<T>(name: string, test: (value: unknown) => boolean): Schema<T> {
  return {
    check(value, path, ctx) {
      if (!test(value)) ctx.fail(path, `应为 ${name}，实际是 ${describe(value)}（${JSON.stringify(value)}）`)
    },
  }
}

export const str = primitive<string>('string', v => typeof v === 'string')
/** 任意有限数（时长、比例、置信度） */
export const num = primitive<number>('number', v => typeof v === 'number' && Number.isFinite(v))
/** 整数（计数、ID、时间戳）：TS 里同是 number，但 Swift / Kotlin 会按整数解码，发了小数它们就挂 */
export const int = primitive<number>('整数', v => typeof v === 'number' && Number.isSafeInteger(v))
export const bool = primitive<boolean>('boolean', v => typeof v === 'boolean')
export const nul = primitive<null>('null', v => v === null)

/** 字面量联合：lit('episode', 'alpha') 的类型是 'episode' | 'alpha' */
export function lit<const V extends readonly (string | number | boolean)[]>(...values: V): Schema<V[number]> {
  return primitive<V[number]>(values.map(v => JSON.stringify(v)).join(' | '), v => values.includes(v as V[number]))
}

export function nullable<T>(inner: Schema<T>): Schema<T | null> {
  return {
    check(value, path, ctx) {
      if (value !== null) inner.check(value, path, ctx)
    },
  }
}

export function arr<T>(inner: Schema<T>): Schema<T[]> {
  return {
    check(value, path, ctx) {
      if (!Array.isArray(value)) {
        ctx.fail(path, `应为数组，实际是 ${describe(value)}`)
        return
      }
      value.forEach((item, i) => inner.check(item, `${path}[${i}]`, ctx))
    },
  }
}

export function record<T>(inner: Schema<T>): Schema<Record<string, T>> {
  return {
    check(value, path, ctx) {
      if (!isPlainObject(value)) {
        ctx.fail(path, `应为对象，实际是 ${describe(value)}`)
        return
      }
      for (const [key, item] of Object.entries(value)) inner.check(item, `${path}.${key}`, ctx)
    },
  }
}

/** 空对象 `{}`：变更类端点的成功载荷（TS 里写作 Record<string, never>） */
export const emptyObject: Schema<Record<string, never>> = {
  check(value, path, ctx) {
    if (!isPlainObject(value)) ctx.fail(path, `应为空对象，实际是 ${describe(value)}`)
    else for (const key of Object.keys(value)) ctx.fail(`${path}.${key}`, '空对象里出现了字段')
  },
}

/**
 * 对象：required 里的字段必须出现，optional 里的可以缺席；其余字段一律算错。
 * 「缺席」与「null」是两回事：可选字段要允许 null 得写 nullable(...)。
 */
// 两个重载而不是一个可选参数：省略 optional 时 O 会被推成约束 Shape，结果类型带上 `[x: string]: any`，
// 「TS 有可选字段、schema 漏了」就查不出来了。
export function obj<R extends Shape>(required: R): Schema<Simplify<{ [K in keyof R]: Infer<R[K]> }>>
export function obj<R extends Shape, O extends Shape>(required: R, optional: O): Schema<ObjType<R, O>>
export function obj(required: Shape, optional?: Shape): AnySchema {
  const opt: Shape = optional ?? {}
  const node: ObjectNode = { optional: Object.keys(opt) }
  if (registerForCoverage && node.optional.length > 0) objectsWithOptional.push(node)
  return {
    check(value, path, ctx) {
      if (!isPlainObject(value)) {
        ctx.fail(path, `应为对象，实际是 ${describe(value)}`)
        return
      }
      let info = ctx.objects.get(node)
      if (info === undefined) {
        info = { path, seen: new Set() }
        ctx.objects.set(node, info)
      }
      for (const [key, schema] of Object.entries(required)) {
        if (!Object.hasOwn(value, key)) ctx.fail(`${path}.${key}`, '缺少必填字段')
        else schema.check(value[key], `${path}.${key}`, ctx)
      }
      for (const [key, item] of Object.entries(value)) {
        if (Object.hasOwn(required, key)) continue
        const schema = opt[key]
        if (schema === undefined) {
          ctx.fail(`${path}.${key}`, '客户端类型里没有这个字段（后端加了字段？同步更新 TS 类型）')
          continue
        }
        info.seen.add(key)
        schema.check(item, `${path}.${key}`, ctx)
      }
    },
  }
}

/** 联合：至少一个分支完全对上（判别联合按 playing / needSelection 这类字段分开） */
export function union<const S extends readonly AnySchema[]>(...members: S): Schema<Infer<S[number]>> {
  const node: UnionNode = { size: members.length }
  return {
    check(value, path, ctx) {
      const attempts = members.map(member => {
        const sub = ctx.child()
        member.check(value, path, sub)
        return sub
      })
      const hit = attempts.findIndex(sub => sub.errors.length === 0)
      let info = ctx.unions.get(node)
      if (info === undefined) {
        info = { path, seen: new Set() }
        ctx.unions.set(node, info)
      }
      if (hit < 0) {
        const details = attempts.map((sub, i) => `分支 ${i + 1}：${sub.errors.join('；')}`).join(' ／ ')
        ctx.fail(path, `没有一个联合分支对得上（${details}）`)
        return
      }
      info.seen.add(hit)
      const winner = attempts[hit]
      if (winner !== undefined) ctx.merge(winner)
    },
  }
}

/** 递归类型（作品的关联作品还是作品）用：推迟到检查时才取目标 schema */
export function lazy<T>(get: () => Schema<T>): Schema<T> {
  return {
    check(value, path, ctx) {
      get().check(value, path, ctx)
    },
  }
}

/**
 * 把 schema 钉到客户端类型 T 上：推出来的类型与 T 不完全一致时多要一个参数，typecheck 因此报错
 * （报错信息里的 mismatch 写着两边各是什么）。返回值按 T 定型，组合进别的 schema 时沿用 T 的名字。
 */
export function exact<T>() {
  return <S extends AnySchema>(
    schema: S,
    ..._mismatch: Equal<Infer<S>, T> extends true ? [] : [mismatch: { schema: Infer<S>; expected: T }]
  ): Schema<T> => schema as unknown as Schema<T>
}

/** 统一信封的成功形态：{ success: true, data } */
export function okEnvelope<T>(data: Schema<T>): Schema<{ success: true; data: T }> {
  return obj({ success: lit(true), data })
}

/** 统一信封的失败形态：{ success: false, data: null, error } */
export const errorEnvelope: Schema<{ success: false; data: null; error: string }> = obj({
  success: lit(false),
  data: nul,
  error: str,
})
