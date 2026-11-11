import { describe, expect, test } from "vitest";
import { NBSP, titleTokens, tokenDelays } from "./titleTokens";

// 横幅标题一块一块地淡入。每块是一个 inline-block，任意两个 inline-block 之间都能断行 ——
// 所以分块规则就是断行规则：拉丁单词拆成单个字母会在词中间折行（「SHE / LL」），
// inline-block 里单独一个普通空格宽度为零。

describe("titleTokens", () => {
  test("每个中日韩字自成一块", () => {
    expect(titleTokens("葬送的芙莉莲")).toEqual(["葬", "送", "的", "芙", "莉", "莲"]);
  });

  test("一串拉丁字母算一块，不会在词中间断开", () => {
    expect(titleTokens("攻壳机动队 THE GHOST")).toEqual([
      "攻", "壳", "机", "动", "队", NBSP, "THE", NBSP, "GHOST",
    ]);
  });

  test("空白不管有几个，都变成一个不断行空格块", () => {
    expect(titleTokens("A  B")).toEqual(["A", NBSP, "B"]);
    expect(titleTokens("A\tB")).toEqual(["A", NBSP, "B"]);
  });

  test("首尾空白去掉", () => {
    expect(titleTokens("  鬼灭  ")).toEqual(["鬼", "灭"]);
  });

  test("拉丁标点和数字跟着所在的词", () => {
    expect(titleTokens("Re:ZERO 4th")).toEqual(["Re:ZERO", NBSP, "4th"]);
    expect(titleTokens("Dr.STONE")).toEqual(["Dr.STONE"]);
  });

  test("带重音的拉丁字母不会把词拆开", () => {
    expect(titleTokens("Pokémon")).toEqual(["Pokémon"]);
  });

  test("全角标点和假名各自成一块", () => {
    expect(titleTokens("Re：从零")).toEqual(["Re", "：", "从", "零"]);
    expect(titleTokens("これ描いて")).toEqual(["こ", "れ", "描", "い", "て"]);
  });

  test("辅助平面的字不会被拆成两个代理项", () => {
    expect(titleTokens("𠮷野家")).toEqual(["𠮷", "野", "家"]);
  });

  test("空标题一块都没有", () => {
    expect(titleTokens("")).toEqual([]);
    expect(titleTokens("   ")).toEqual([]);
  });

  test("各块拼回去就是原标题（空白已规整）", () => {
    const title = "BLEACH 千年血戦篇-禍進譚-";
    expect(titleTokens(title).join("").replaceAll(NBSP, " ")).toBe(title);
  });
});

describe("tokenDelays", () => {
  test("先等一段起步时间，之后每块错开 26ms", () => {
    expect(tokenDelays(0)).toEqual({ switchMs: 160, entryMs: 420 });
    expect(tokenDelays(10)).toEqual({ switchMs: 420, entryMs: 680 });
  });

  test("有上限，长标题不会让读者一直等", () => {
    expect(tokenDelays(200)).toEqual({ switchMs: 900, entryMs: 1200 });
  });
});
