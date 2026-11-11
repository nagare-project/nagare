import { describe, expect, test } from "vitest";
import {
  DRAG_THRESHOLD_PX,
  ENTER_ORDER_CAP,
  FLING_REST_MS,
  GLIDE_FRICTION,
  GLIDE_MIN_SPEED,
  THUMB_MIN_PCT,
  beginPress,
  enterOrder,
  glideStep,
  grabOffset,
  itemsInView,
  maxScrollLeft,
  movePress,
  nearestStop,
  nearestStopIndex,
  nowScrollLeft,
  pageScrollLeft,
  railEdges,
  railStops,
  releaseVelocity,
  releasedElsewhere,
  scrollForPointer,
  startsDrag,
  stepStop,
  takesOverFling,
  suppressesClick,
  thumbFor,
} from "./railScroll";

// 今日更新那一排：用鼠标拖（按下后过了门槛就是拖动，不能打开按下时那张卡；快速拖动后惯性滑完、
// 停在一张卡上），用下面的滑块移动（拖、点，或用键盘一次一张卡、一次一页），一打开就滚到「现在」。

describe("startsDrag", () => {
  test("只有鼠标左键能拖动", () => {
    expect(startsDrag("mouse", 0)).toBe(true);
    expect(startsDrag("mouse", 1)).toBe(false);
    expect(startsDrag("mouse", 2)).toBe(false);
  });

  test("触摸和笔走浏览器自己的滚动，不经过拖动", () => {
    expect(startsDrag("touch", 0)).toBe(false);
    expect(startsDrag("pen", 0)).toBe(false);
  });
});

describe("releasedElsewhere", () => {
  test("左键已松开时的移动不属于任何一次按下", () => {
    expect(releasedElsewhere(0)).toBe(true);
    // 只按着右键或中键：仍然不是我们的那次按下。
    expect(releasedElsewhere(2)).toBe(true);
    expect(releasedElsewhere(4)).toBe(true);
  });

  test("左键还按着（单独或连同别的键）：这次按下还在继续", () => {
    expect(releasedElsewhere(1)).toBe(false);
    expect(releasedElsewhere(3)).toBe(false);
  });
});

describe("在横排上按下", () => {
  const press = beginPress(7, 500, 120);

  test("小幅抖动仍算点击", () => {
    const moved = movePress(press, 500 + DRAG_THRESHOLD_PX);
    expect(moved.press.dragging).toBe(false);
    expect(moved.started).toBe(false);
    expect(moved.scrollLeft).toBeNull();
    expect(suppressesClick(moved.press)).toBe(false);
  });

  test("过了门槛就变成拖动（只变一次），横排跟着指针走", () => {
    const first = movePress(press, 500 - (DRAG_THRESHOLD_PX + 1));
    expect(first.started).toBe(true);
    expect(first.press.dragging).toBe(true);
    // 往左拖 7，就往后滚 7。
    expect(first.scrollLeft).toBe(127);

    const second = movePress(first.press, 300);
    expect(second.started).toBe(false);
    expect(second.scrollLeft).toBe(320);
  });

  test("往右拖是往开头方向滚回去", () => {
    expect(movePress(press, 600).scrollLeft).toBe(20);
  });

  test("拖动过就拦下松开时的点击 —— 哪怕又拖回了原处", () => {
    const out = movePress(press, 400).press;
    const back = movePress(out, 500).press;
    expect(back.travel).toBe(100);
    expect(suppressesClick(back)).toBe(true);
    // 横排也跟着回来。
    expect(movePress(out, 500).scrollLeft).toBe(120);
  });

  test("停住滑动中横排的那一下什么都不打开，哪怕没出过门槛", () => {
    const still = movePress(press, 500 + 2).press;
    expect(suppressesClick(still, true)).toBe(true);
    expect(suppressesClick(still, false)).toBe(false);
  });

  test("一直没出门槛的按下放行点击", () => {
    let p = press;
    for (const x of [502, 497, 503, 499]) p = movePress(p, x).press;
    expect(suppressesClick(p)).toBe(false);
  });
});

describe("railEdges", () => {
  test("可滚的横排在开头", () => {
    expect(railEdges({ viewLeft: 0, viewWidth: 1000, contentWidth: 2600 })).toEqual({
      overflow: true,
      atStart: true,
      atEnd: false,
    });
  });

  test("在中间", () => {
    expect(railEdges({ viewLeft: 800, viewWidth: 1000, contentWidth: 2600 })).toEqual({
      overflow: true,
      atStart: false,
      atEnd: false,
    });
  });

  test("在末尾，容得下小数滚动位置", () => {
    expect(railEdges({ viewLeft: 1599.5, viewWidth: 1000, contentWidth: 2600 })).toEqual({
      overflow: true,
      atStart: false,
      atEnd: true,
    });
  });

  test("放得下的横排没有可翻的", () => {
    const edges = railEdges({ viewLeft: 0, viewWidth: 1440, contentWidth: 1440 });
    expect(edges.overflow).toBe(false);
    expect(maxScrollLeft({ viewLeft: 0, viewWidth: 1440, contentWidth: 900 })).toBe(0);
  });
});

// 1440px 的桌面：两侧留白 80px，卡宽 148px、间距 16px。
const PAD = 80;
const STEP = 164;
const cards = (n: number) => Array.from({ length: n }, (_, k) => PAD + k * STEP);
const desk = (viewLeft: number, n = 20) => ({
  viewLeft,
  viewWidth: 1440,
  // 最后一张卡的右边缘加上右侧留白。
  contentWidth: PAD + (n - 1) * STEP + 148 + PAD,
});

describe("pageScrollLeft", () => {
  test("往后翻：被右边切到的那张成为第一张", () => {
    // 第 0–6 张放得下（第 6 张止于 1212 ≤ 1360）；第 7 张（1228–1376）被切到。
    const target = pageScrollLeft(desk(0), cards(20), PAD, 1);
    expect(target).toBe(cards(20)[7] - PAD);
  });

  test("从那里往前翻回到开头", () => {
    expect(pageScrollLeft(desk(1148), cards(20), PAD, -1)).toBe(0);
  });

  test("从中间往后翻再往前翻，两次对称", () => {
    const forward = pageScrollLeft(desk(1148, 30), cards(30), PAD, 1);
    expect(forward).toBe(cards(30)[14] - PAD);
    expect(pageScrollLeft(desk(forward, 30), cards(30), PAD, -1)).toBe(1148);
  });

  test("不会翻过任何一端", () => {
    const g = desk(2000);
    expect(pageScrollLeft(g, cards(20), PAD, 1)).toBe(maxScrollLeft(g));
    expect(pageScrollLeft(desk(100), cards(20), PAD, -1)).toBe(0);
  });

  test("「现在」标记比卡窄，也和别的项一样能当落点", () => {
    // 五张卡、44px 的标记，然后又是卡。
    const starts = [...cards(5), PAD + 5 * STEP, ...cards(10).map((s) => s + 5 * STEP + 60)];
    const target = pageScrollLeft(desk(0), starts, PAD, 1);
    expect(starts.map((s) => s - PAD)).toContain(target);
    expect(target).toBeGreaterThan(0);
    expect(target).toBeLessThanOrEqual(1440 - 2 * PAD);
  });

  test("比屏还宽的项照样让横排翻一整页", () => {
    expect(pageScrollLeft({ viewLeft: 0, viewWidth: 400, contentWidth: 3000 }, [20, 2020], 20, 1)).toBe(360);
  });
});

describe("nowScrollLeft：横排一打开停在哪里", () => {
  test("早上：标记和下一部已经在屏上，不滚", () => {
    // 两张已播、标记，其余待播。
    const now = { markerStart: PAD + 2 * STEP, prevStart: PAD + STEP, nextEnd: PAD + 2 * STEP + 60 + 148 };
    expect(nowScrollLeft(desk(0), now, PAD)).toBeNull();
  });

  test("桌面上的晚上：最后一张已播的贴着留白，接着是「现在」和下一部", () => {
    const prevStart = PAD + 9 * STEP;
    const markerStart = prevStart + STEP;
    const now = { markerStart, prevStart, nextEnd: markerStart + 60 + 148 };
    expect(nowScrollLeft(desk(0), now, PAD)).toBe(prevStart - PAD);
  });

  test("手机上放不下三样：标记贴着留白，好让下一部放得下", () => {
    // 390px，留白 20px，卡宽 148px、间距 12px；三张已播。
    const phone = { viewLeft: 0, viewWidth: 390, contentWidth: 20 + 12 * 160 + 20 };
    const prevStart = 20 + 2 * 160;
    const markerStart = prevStart + 160;
    const now = { markerStart, prevStart, nextEnd: markerStart + 56 + 148 };
    expect(nowScrollLeft(phone, now, 20)).toBe(markerStart - 20);
  });

  test("全都播完：滚到末尾，标记就在那里", () => {
    const n = 15;
    const g = desk(0, n);
    const prevStart = PAD + (n - 1) * STEP;
    const now = { markerStart: prevStart + STEP, prevStart, nextEnd: prevStart + STEP + 44 };
    expect(nowScrollLeft(g, now, PAD)).toBe(Math.min(prevStart - PAD, maxScrollLeft(g)));
  });

  test("一集都还没播：标记打头，不滚", () => {
    expect(nowScrollLeft(desk(0), { markerStart: PAD, prevStart: null, nextEnd: PAD + 60 + 148 }, PAD)).toBeNull();
  });
});

describe("thumbFor：滑块的宽度与位置", () => {
  test("宽度等于屏上所占比例；横排在开头时它也在开头", () => {
    const g = desk(0);
    const thumb = thumbFor(g);
    expect(thumb.widthPct).toBeCloseTo((1440 / g.contentWidth) * 100, 6);
    expect(thumb.leftPct).toBe(0);
  });

  test("横排滚到底时滑块在轨道最右端", () => {
    const g = desk(0);
    const end = thumbFor({ ...g, viewLeft: maxScrollLeft(g) });
    expect(end.leftPct + end.widthPct).toBeCloseTo(100, 6);
  });

  test("横排滚到一半，滑块走到轨道可移动范围的一半", () => {
    const g = desk(0);
    const half = thumbFor({ ...g, viewLeft: maxScrollLeft(g) / 2 });
    expect(half.leftPct).toBeCloseTo((100 - half.widthPct) / 2, 6);
  });

  test("不窄于下限，一天排得很长也抓得住", () => {
    const long = { viewLeft: 0, viewWidth: 390, contentWidth: 390 * 100 };
    expect(thumbFor(long).widthPct).toBe(THUMB_MIN_PCT);
    expect(thumbFor(long, 10).widthPct).toBe(10);
  });

  test("放得下的横排：滑块占满轨道，无处可移", () => {
    expect(thumbFor({ viewLeft: 0, viewWidth: 1440, contentWidth: 900 })).toEqual({ widthPct: 100, leftPct: 0 });
  });

  test("小数位置越过两端时滑块仍留在轨道上", () => {
    const g = desk(0);
    expect(thumbFor({ ...g, viewLeft: -3 }).leftPct).toBe(0);
    const over = thumbFor({ ...g, viewLeft: maxScrollLeft(g) + 3 });
    expect(over.leftPct + over.widthPct).toBeCloseTo(100, 6);
  });
});

describe("拖动滑块", () => {
  const TRACK = 1280;
  const thumb = { widthPct: 25, leftPct: 30 };

  test("按在滑块上：抓住的那一点一直在指针下面", () => {
    // 在 1280px 的轨道上，滑块占 384–704。
    expect(grabOffset(thumb, TRACK, 400)).toEqual({ onThumb: true, grab: 16 });
    expect(grabOffset(thumb, TRACK, 704)).toEqual({ onThumb: true, grab: 320 });
  });

  test("按在空轨道上：抓住滑块中点", () => {
    expect(grabOffset(thumb, TRACK, 100)).toEqual({ onThumb: false, grab: 160 });
    expect(grabOffset(thumb, TRACK, 1200)).toEqual({ onThumb: false, grab: 160 });
  });

  test("指针移动，横排按比例跟着移动，移回来也一样", () => {
    const g = desk(0);
    const t = thumbFor(g);
    const room = TRACK * (1 - t.widthPct / 100);
    const grab = 10;
    expect(scrollForPointer(g, t, TRACK, grab, grab)).toBe(0);
    expect(scrollForPointer(g, t, TRACK, grab + room / 2, grab)).toBeCloseTo(maxScrollLeft(g) / 2, 6);
    expect(scrollForPointer(g, t, TRACK, grab + room, grab)).toBeCloseTo(maxScrollLeft(g), 6);
  });

  test("指针越过轨道两端时，横排停在自己的两端", () => {
    const g = desk(0);
    const t = thumbFor(g);
    expect(scrollForPointer(g, t, TRACK, -500, 10)).toBe(0);
    expect(scrollForPointer(g, t, TRACK, 5000, 10)).toBe(maxScrollLeft(g));
  });

  test("占满轨道的滑块拖了也不动", () => {
    const fits = { viewLeft: 0, viewWidth: 1440, contentWidth: 900 };
    expect(scrollForPointer(fits, thumbFor(fits), TRACK, 600, 10)).toBe(0);
  });
});

describe("横排停在哪里", () => {
  test("停靠点是每一项贴着留白的位置，以及两端", () => {
    const g = desk(0, 20);
    const stops = railStops(g, cards(20), PAD);
    expect(stops[0]).toBe(0);
    expect(stops[stops.length - 1]).toBe(maxScrollLeft(g));
    expect(stops).toContain(cards(20)[3] - PAD);
    // 升序、不重复、不超过末尾。
    expect([...stops].sort((a, b) => a - b)).toEqual(stops);
    expect(new Set(stops).size).toBe(stops.length);
    expect(Math.max(...stops)).toBe(maxScrollLeft(g));
  });

  test("在两张卡之间松开，落到近的那张", () => {
    const stops = railStops(desk(0, 20), cards(20), PAD);
    expect(nearestStop(3 * STEP + 40, stops)).toBe(3 * STEP);
    expect(nearestStop(3 * STEP + 120, stops)).toBe(4 * STEP);
  });

  test("越过最后一张卡的停靠点松开，落到末尾", () => {
    const g = desk(0, 20);
    const stops = railStops(g, cards(20), PAD);
    expect(nearestStop(maxScrollLeft(g) - 2, stops)).toBe(maxScrollLeft(g));
  });

  test("滑块的方向键一次走一个停靠点，到头就不动", () => {
    const g = desk(0, 20);
    const stops = railStops(g, cards(20), PAD);
    expect(stepStop(g, stops, 1)).toBe(STEP);
    expect(stepStop({ ...g, viewLeft: STEP }, stops, 1)).toBe(2 * STEP);
    expect(stepStop({ ...g, viewLeft: STEP + 30 }, stops, -1)).toBe(STEP);
    expect(stepStop(g, stops, -1)).toBe(0);
    const end = { ...g, viewLeft: maxScrollLeft(g) };
    expect(stepStop(end, stops, 1)).toBe(maxScrollLeft(g));
  });
});

describe("惯性滑动", () => {
  const samples = [
    { t: 1000, x: 900 },
    { t: 1016, x: 860 },
    { t: 1032, x: 820 },
    { t: 1048, x: 780 },
  ];

  test("松开时的速度按最后几次移动算，单位 px/ms", () => {
    expect(releaseVelocity(samples, 1050)).toBeCloseTo(-120 / 48, 6);
  });

  test("松开前指针已经停住，就没有惯性", () => {
    expect(releaseVelocity(samples, 1048 + FLING_REST_MS + 1)).toBe(0);
  });

  test("只有一个采样或没有采样，速度为零", () => {
    expect(releaseVelocity([], 0)).toBe(0);
    expect(releaseVelocity([{ t: 5, x: 5 }], 6)).toBe(0);
  });

  test("只算最近的移动：早先慢慢起步不会拉低速度", () => {
    const late = [{ t: 0, x: 1000 }, ...samples];
    expect(releaseVelocity(late, 1050)).toBeCloseTo(-120 / 48, 6);
  });

  test("每一帧按速度移动横排并损失一部分速度，帧越长损失越多", () => {
    const one = glideStep(2, 16);
    expect(one.dx).toBe(32);
    expect(one.velocity).toBeCloseTo(2 * GLIDE_FRICTION, 9);
    const two = glideStep(2, 32);
    expect(two.velocity).toBeCloseTo(2 * GLIDE_FRICTION * GLIDE_FRICTION, 9);
  });

  test("惯性在合理的时间和距离内停下", () => {
    let v = 3; // 快速一甩
    let travelled = 0;
    let frames = 0;
    while (Math.abs(v) >= GLIDE_MIN_SPEED && frames < 1000) {
      const step = glideStep(v, 16);
      travelled += step.dx;
      v = step.velocity;
      frames += 1;
    }
    expect(frames).toBeLessThan(90); // 不到 1.5 秒
    expect(travelled).toBeGreaterThan(400);
    expect(travelled).toBeLessThan(1200);
  });
});

describe("itemsInView：滑块念给读者的「正在看哪几项」", () => {
  const boxes = (n: number) => cards(n).map((start) => ({ start, width: 148 }));

  test("桌面横排在开头：中点落在两侧留白之间的卡", () => {
    // 第 7 张占 1228–1376：中点（1302）没到右侧留白（1360）；第 8 张的中点越过了。
    expect(itemsInView(desk(0), boxes(20), PAD)).toEqual({ first: 1, last: 8 });
  });

  test("滚动之后：中点进了左侧留白的卡不再算", () => {
    expect(itemsInView(desk(STEP * 3 + 120), boxes(20), PAD)).toEqual({ first: 5, last: 12 });
  });

  test("空横排没有可念的项", () => {
    expect(itemsInView(desk(0), [], PAD)).toBeNull();
  });
});

describe("enterOrder：卡片浮起的顺序", () => {
  const boxes = (n: number) => cards(n).map((start) => ({ start, width: 148 }));

  test("屏上的卡从左到右浮起，其余的排在最后", () => {
    const order = enterOrder(desk(STEP * 4), boxes(20));
    expect(order.slice(4, 12)).toEqual([0, 1, 2, 3, 4, 5, 6, 7]);
    expect(order[0]).toBe(ENTER_ORDER_CAP);
    expect(order[19]).toBe(ENTER_ORDER_CAP);
  });

  test("不管屏上有多少张，都不超过上限", () => {
    const wide = { viewLeft: 0, viewWidth: 5000, contentWidth: 5000 };
    expect(Math.max(...enterOrder(wide, boxes(30)))).toBe(ENTER_ORDER_CAP);
  });
});

describe("滑块的 range 输入数的是停靠点，不是像素", () => {
  test("它的值是离横排当前位置最近的停靠点的下标", () => {
    const stops = railStops(desk(0, 20), cards(20), PAD);
    expect(nearestStopIndex(0, stops)).toBe(0);
    expect(nearestStopIndex(3 * STEP + 40, stops)).toBe(3);
    expect(nearestStopIndex(3 * STEP + 120, stops)).toBe(4);
    expect(nearestStopIndex(1e6, stops)).toBe(stops.length - 1);
  });

  test("没有停靠点时下标为 0", () => {
    expect(nearestStopIndex(50, [])).toBe(0);
  });
});

describe("takesOverFling：惯性滑动中的横排上来了一次滚轮", () => {
  test("横向滚动（触控板）接管横排", () => {
    expect(takesOverFling(30, 4)).toBe(true);
    expect(takesOverFling(-30, 0)).toBe(true);
  });

  test("竖向滚轮滚动页面，让惯性自己走完", () => {
    expect(takesOverFling(0, 100)).toBe(false);
    expect(takesOverFling(5, -40)).toBe(false);
  });
});
