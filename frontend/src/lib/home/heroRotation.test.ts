import { describe, expect, test } from "vitest";
import {
  FRESH_COUNTDOWN,
  HERO_INTERVAL_MS,
  HERO_PRELOAD_LEAD_MS,
  canRotate,
  countdownFor,
  countdownKey,
  countdownTimers,
  holdCountdown,
  isHeld,
  nextSlide,
  remainingMs,
  resumeCountdown,
  type HeroHolds,
} from "./heroRotation";

// 横幅的自动切换，按 WCAG 2.2.2：每一种能停住它的原因、停住时暂停而不是重来的倒计时、
// 切换和回到标签页引起的从头计时，以及它挂的两个定时器。

const FREE: HeroHolds = { pointer: false, focus: false, paused: false, hidden: false, offscreen: false };

describe("isHeld", () => {
  test("没有任何东西停住它：横幅照常切换", () => {
    expect(isHeld(FREE)).toBe(false);
  });

  test.each(Object.keys(FREE) as Array<keyof HeroHolds>)("单是 %s 就能停住它", (reason) => {
    expect(isHeld({ ...FREE, [reason]: true })).toBe(true);
  });

  test("所有停住的原因都结束之前一直停着", () => {
    // 鼠标离开了横幅，但键盘焦点还在里面。
    expect(isHeld({ ...FREE, focus: true })).toBe(true);
    expect(isHeld({ ...FREE, pointer: true, focus: true })).toBe(true);
  });
});

describe("canRotate", () => {
  test("两张及以上、允许动态效果时会切换", () => {
    expect(canRotate(5, false)).toBe(true);
    expect(canRotate(2, false)).toBe(true);
  });

  test("减弱动态效果时不切，不管有几张", () => {
    expect(canRotate(5, true)).toBe(false);
  });

  test("没有可切换的下一张时不切", () => {
    expect(canRotate(1, false)).toBe(false);
    expect(canRotate(0, false)).toBe(false);
  });
});

describe("倒计时", () => {
  test("新的倒计时剩满整段间隔", () => {
    expect(remainingMs(FRESH_COUNTDOWN, 123_456)).toBe(HERO_INTERVAL_MS);
  });

  test("正在走的倒计时从恢复那一刻往下数", () => {
    const running = resumeCountdown(FRESH_COUNTDOWN, 1_000);
    expect(remainingMs(running, 1_000)).toBe(8_000);
    expect(remainingMs(running, 4_000)).toBe(5_000);
  });

  test("停住是暂停：已经走过的时间记下，不丢掉", () => {
    const running = resumeCountdown(FRESH_COUNTDOWN, 0);
    const held = holdCountdown(running, 3_000);
    expect(held).toEqual({ spentMs: 3_000, runningSince: null });
    // 停了一分钟：一点没动。
    expect(remainingMs(held, 63_000)).toBe(5_000);
    // 恢复后：接着剩下的 5 秒走。
    const resumed = resumeCountdown(held, 63_000);
    expect(remainingMs(resumed, 65_000)).toBe(3_000);
  });

  test("多次停住累计", () => {
    let c = resumeCountdown(FRESH_COUNTDOWN, 0);
    c = holdCountdown(c, 2_000);
    c = resumeCountdown(c, 10_000);
    c = holdCountdown(c, 13_000);
    expect(c.spentMs).toBe(5_000);
    expect(remainingMs(c, 99_000)).toBe(3_000);
  });

  test("对正在走的再恢复、对已停住的再停住，都不变", () => {
    const running = resumeCountdown(FRESH_COUNTDOWN, 500);
    expect(resumeCountdown(running, 2_000)).toBe(running);
    const held = holdCountdown(running, 1_500);
    expect(holdCountdown(held, 9_000)).toBe(held);
  });

  test("剩余时间不会是负数", () => {
    const running = resumeCountdown(FRESH_COUNTDOWN, 0);
    expect(remainingMs(running, 20_000)).toBe(0);
    // 时钟往回走也不会多出时间。
    expect(remainingMs(running, -5_000)).toBe(HERO_INTERVAL_MS);
  });
});

describe("countdownTimers", () => {
  test("新的一张在切换前不久预先加载下一张的横幅图", () => {
    const running = resumeCountdown(FRESH_COUNTDOWN, 0);
    expect(countdownTimers(running, 0)).toEqual({
      preloadInMs: HERO_INTERVAL_MS - HERO_PRELOAD_LEAD_MS,
      advanceInMs: HERO_INTERVAL_MS,
    });
    expect(HERO_PRELOAD_LEAD_MS).toBeLessThan(HERO_INTERVAL_MS);
  });

  test("间隔快到头时才恢复：立刻预加载", () => {
    const late = resumeCountdown({ spentMs: 7_000, runningSince: null }, 50_000);
    expect(countdownTimers(late, 50_000)).toEqual({ preloadInMs: 0, advanceInMs: 1_000 });
  });
});

describe("nextSlide", () => {
  test("往后走一张，最后一张之后回到第一张", () => {
    expect(nextSlide(0, 5)).toBe(1);
    expect(nextSlide(3, 5)).toBe(4);
    expect(nextSlide(4, 5)).toBe(0);
  });

  test("一张都没有时原地不动", () => {
    expect(nextSlide(0, 0)).toBe(0);
  });
});

describe("countdownFor：什么会让 8 秒从头算", () => {
  const banked = { spentMs: 6_000, runningSince: null };

  test("同一张、同一次回到标签页：接着走", () => {
    const key = countdownKey(2, 0);
    expect(countdownFor({ key, countdown: banked }, key)).toBe(banked);
  });

  test("换了一张（手动或到点切换）：从头算", () => {
    expect(countdownFor({ key: countdownKey(2, 0), countdown: banked }, countdownKey(3, 0))).toBe(FRESH_COUNTDOWN);
  });

  test("同一张，但标签页从后台回来过：从头算", () => {
    expect(countdownFor({ key: countdownKey(2, 0), countdown: banked }, countdownKey(2, 1))).toBe(FRESH_COUNTDOWN);
  });

  test("还没有记下的：新的倒计时", () => {
    expect(countdownFor(null, countdownKey(0, 0))).toBe(FRESH_COUNTDOWN);
  });

  test("key 分得清是哪一张、第几次回到标签页", () => {
    expect(countdownKey(1, 12)).not.toBe(countdownKey(11, 2));
  });
});
