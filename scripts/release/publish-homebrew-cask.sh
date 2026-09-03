#!/usr/bin/env bash
# 把 packaging/homebrew/Casks/nagare.rb 的 version 与 sha256 填成本次发布的真实值，
# 推到 Homebrew tap 仓库 nagare-project/homebrew-nagare。
#
# 用法：scripts/release/publish-homebrew-cask.sh <版本，不带 v> <checksums.txt 路径>
# 例：  scripts/release/publish-homebrew-cask.sh 0.2.0 dist/checksums.txt
#
# 环境变量：
#   TAP_GITHUB_TOKEN     跨仓库推送用的 PAT。**没配就警告 + 退出 0**（Homebrew 是可选
#                        渠道，缺它不该让一次正式发布变红），渲染结果仍然落到
#                        dist/homebrew/Casks/nagare.rb 供人工补推。
#   NAGARE_TAP_REPO      覆盖 tap 仓库（默认 nagare-project/homebrew-nagare）。
#   NAGARE_CASK_DRY_RUN  =1 时只渲染不推送，配了 token 也不推。本机验证用。
#
# ── 为什么不用 GoReleaser 的 homebrew_casks（v2.18.0 实测，2026-09-03）─────────────
# 三条都撞死了，不是配置姿势问题：
#   1. cask pipe 只从 `archives:` 产出的归档里挑源。dmg 与 .app.zip 是
#      universal_binaries 的 post hook（scripts/macos/build-app.sh）生成、经
#      release.extra_files 挂上去的，根本不在候选集里；喂 meta 归档会直接报
#      "no linux/macos archives found matching goos=[darwin linux]"。
#   2. `url.template` 能改下载地址，但 `sha256` 仍然取自它挑中的那个归档 ——
#      指向 dmg 会配上 macOS tar.gz 的 sha256，brew 每次校验都失败。
#   3. 它的 cask 模板里**没有 app 这一段**，能生成的只有 `binary "nagare"`：
#      装出来是个裸二进制，没有 .app、没有菜单栏、没有 LSUIElement。
# 唯一能让它原生跑通的做法是把 Nagare.app 塞进 macOS tar.gz（体积翻倍，还要重新
# 验证签名能不能扛住 goreleaser 自己的打包器）——为一个可选渠道改主产物，不划算。
# 于是 cask 手写，这个脚本只替换那两行会变的值。
set -euo pipefail

fail() { echo "$*" >&2; exit 1; }

[ $# -eq 2 ] || fail "用法：$0 <版本，不带 v> <checksums.txt 路径>"
version=$1
manifest=$2

repo=${NAGARE_TAP_REPO:-nagare-project/homebrew-nagare}
root=$(cd "$(dirname "$0")/../.." && pwd)
template="$root/packaging/homebrew/Casks/nagare.rb"

# 版本号与哈希都会被写进一个 Ruby 源文件，所以形状要先钉死 —— 这一步【必须】排在
# 预发布判定之前：先按 semver 认形状，认不出来的一律报错退出，别让
# 「0.2.0; rm -rf /」这种输入因为里面带个 - 就走进「预发布，跳过」那条静默路径。
echo "${version}" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$' \
  || fail "版本号形状不对：${version}（要 x.y.z，可带 -rc.1 之类后缀）"

# 预发布不推 tap：rc / beta 的 dmg 不该落进稳定渠道，
# 那会把所有 `brew install --cask nagare` 的人一起带上预览版。
# 与 .goreleaser.yaml 的 release.prerelease=auto、scoops.skip_upload=auto 同一条规矩。
# 走到这一行之后，${version} 必定是纯 x.y.z。
case "${version}" in
  *[-+]*) echo "==> ${version} 是预发布版本，跳过 Homebrew tap"; exit 0 ;;
esac

[ -f "$template" ] || fail "找不到 cask 模板：$template"
[ -f "$manifest" ] || fail "找不到校验清单：$manifest"

# cask 装的是 dmg。名字与 .goreleaser.yaml + scripts/macos/build-app.sh 的产物名
# 逐字一致；对不上就在这里红，而不是让用户 brew install 时 404。
dmg="nagare-${version}_MacOS_universal.dmg"
sha=$(awk -v want="$dmg" '$2 == want || $2 == "*" want { print $1; exit }' "$manifest")
[ -n "$sha" ] || fail "校验清单 $manifest 里没有 $dmg —— 版本号对吗？dmg 构建成功了吗？"
echo "$sha" | grep -qE '^[0-9a-f]{64}$' || fail "从清单里取到的不是 sha256：$sha"

work=$(mktemp -d)
# trap 里显式把退出码带出去：bash 3.2（macOS runner 自带的那个）在 `set -u` 撞到
# 未定义变量时，只要挂了 EXIT trap，最终退出码会被 trap 里最后一条命令的 0 顶掉 ——
# 实测过。不修的话脚本挂了 CI 还是绿的，而 tap 停在旧版本没人知道。
trap 'ec=$?; rm -rf "$work"; exit "$ec"' EXIT
rendered="$work/nagare.rb"

# 只替换行首锚定的那两行。url 那行里也有 #{version}，但它以两空格 + url 开头，
# 不会被 `^  version "` 命中。
sed -e "s|^  version \".*\"\$|  version \"${version}\"|" \
    -e "s|^  sha256 \".*\"\$|  sha256 \"${sha}\"|" \
    "$template" > "$rendered"

# 替换是否真的发生了要验一遍：sed 匹配不上是**静默**的，不验的话会推一个还带着
# 0.0.0 / 全零哈希的 cask 上去，而那玩意儿装不了。模板改了形状就在这里红。
grep -qx "  version \"${version}\"" "$rendered" || fail "版本没写进 cask —— 模板里 version 那一行的形状变了？"
grep -qx "  sha256 \"${sha}\"" "$rendered"     || fail "sha256 没写进 cask —— 模板里 sha256 那一行的形状变了？"
if grep -q '"0\{64\}"' "$rendered"; then
  fail "渲染后 cask 里还留着占位哈希，不推"
fi

if command -v ruby >/dev/null 2>&1; then
  ruby -c "$rendered" >/dev/null || fail "渲染出来的 cask 不是合法 Ruby"
fi

# 渲染结果永远留一份在 dist/，无论推不推：没配 token 时它就是人工补推的输入。
out="$root/dist/homebrew/Casks"
mkdir -p "$out"
cp "$rendered" "$out/nagare.rb"
# 注意：紧挨着中文标点的变量一律写成 ${x}。bash 3.2 在非 UTF-8 locale 下会把全角标点
# 的高位字节当成变量名的一部分，$version， 会变成「未定义变量」而不是「version 后跟一个逗号」。
echo "==> 已渲染：dist/homebrew/Casks/nagare.rb（nagare ${version}，dmg sha256 ${sha:0:12}…）"

if [ "${NAGARE_CASK_DRY_RUN:-}" = "1" ]; then
  echo "==> NAGARE_CASK_DRY_RUN=1，不推送"
  exit 0
fi

if [ -z "${TAP_GITHUB_TOKEN:-}" ]; then
  # 与 MINISIGN_SECRET_KEY 同一套处理：警告 + 跳过，绝不阻断发布。
  # Homebrew 是可选渠道，缺它只是 tap 停在旧版本，手动补的步骤见 docs/releasing.md。
  echo "::warning::未配置 TAP_GITHUB_TOKEN，未更新 Homebrew tap —— brew 用户仍停在旧版本（见 docs/releasing.md）"
  exit 0
fi

echo "==> 克隆 tap 仓库 $repo"
# 匿名克隆：tap 必须是公开仓库（brew tap 就是匿名克隆它），
# 这样 token 不会被写进 .git/config。
git clone --depth 1 "https://github.com/${repo}.git" "$work/tap" \
  || fail "克隆 $repo 失败 —— 仓库建了吗？是公开的吗？"

mkdir -p "$work/tap/Casks"
install -m 0644 "$rendered" "$work/tap/Casks/nagare.rb"

if [ -z "$(git -C "$work/tap" status --porcelain -- Casks/nagare.rb)" ]; then
  echo "==> cask 与仓库里的一致，无需推送"
  exit 0
fi

# 提交信息用 Homebrew tap 的惯例「<token> <版本>」，不是本仓库的 conventional commits。
git -C "$work/tap" add Casks/nagare.rb
git -C "$work/tap" \
  -c user.name="nagare release" \
  -c user.email="noreply@github.com" \
  commit -q -m "nagare ${version}"

# token 只落进一个 0600 的临时凭证文件，不进 argv（ps 看得到）、不进 .git/config。
# work 目录由上面的 trap 删掉。
umask 077
printf 'https://x-access-token:%s@github.com\n' "$TAP_GITHUB_TOKEN" > "$work/.git-credentials"
if ! git -C "$work/tap" \
      -c credential.helper="store --file=$work/.git-credentials" \
      push -q origin HEAD; then
  # 不回显 token，也不回显 git 拼出来的带凭证 URL。
  fail "推送 $repo 失败 —— PAT 过期了？scope 里有没有勾上这个仓库的 Contents: Read and write？"
fi

echo "==> 已推送 nagare $version 到 $repo"
