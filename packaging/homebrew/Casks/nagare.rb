# nagare 的 Homebrew cask。这个文件住在 tap 仓库 nagare-project/homebrew-nagare 的
# Casks/ 下，本仓库里的这一份是它的**唯一来源**：每次正式发版由
# scripts/release/publish-homebrew-cask.sh 把 version 与 sha256 两行填成真实值再推过去。
#
# 手写而不是让 goreleaser 生成，理由见那个脚本开头（goreleaser v2 的 homebrew_casks
# 只认 archives 产出的归档，配不出「装 .app」的 cask）。
#
# ⚠️ version 与 sha256 两行的形状（行首两个空格 + 关键字 + 双引号）是脚本的替换锚点。
# 改了形状脚本会直接报错，而不是悄悄推一个填不进去的 cask —— 这是有意的。
cask "nagare" do
  # 占位值。真实版本号与 dmg 的 sha256 由发版脚本从 checksums.txt 里取。
  version "0.0.0"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"

  # 源用 dmg 而不是 .app.zip：
  #   1. dmg 是 cask 最常规的形态，挂载与卸载由 Homebrew 自己处理，不需要额外 stanza；
  #   2. 它就是 README 让用户手动下载的那一个 —— brew 装的和手动装的是同一份字节，
  #      出问题时只有一条链路要查；
  #   3. .app.zip 留给自更新专用（internal/selfupdate/assets.go 按名字取它），
  #      一个产物一个消费者，改动时不会互相牵连。
  url "https://github.com/nagare-project/nagare/releases/download/v#{version}/nagare-#{version}_MacOS_universal.dmg"
  name "nagare"
  desc "Local anime playback agent that drives mpv from a browser UI"
  homepage "https://github.com/nagare-project/nagare"

  livecheck do
    url :url
    strategy :github_latest
  end

  # nagare 自己带更新器，而 cask 装出来的就是 /Applications/Nagare.app —— 与 dmg 装的
  # 完全同形，所以应用内「立即更新」会直接整包替换它，brew 记录的版本号随之过时。
  # auto_updates true 就是把这件事如实声明出来：`brew upgrade` 默认不再管它，
  # 想让 brew 的记录追上用 `brew upgrade --cask --greedy nagare`。
  # 不声明的话两个更新器会互相覆盖，用户看到的是「升级完版本号还是旧的」。
  auto_updates true
  # ⚠️ auto_updates 与 depends_on 属于同一个 stanza 组，中间【不能】空行
  # （brew style 的 Cask/StanzaGrouping 会报错）。
  #
  # 走 Homebrew 这条渠道最大的好处：mpv 一起装上。
  # macOS 上 nagare 不捆绑 mpv（决议 A5），手动装 dmg 的用户得自己 brew install mpv。
  # macos 下限与产物的部署目标一致：Ventura = macOS 13（见 .goreleaser.yaml 与 Info.plist）。
  # 裸符号在 cask 里就是「不低于」的意思，写 ">= :ventura" 会被 brew style 要求改回来。
  depends_on formula: "mpv",
             macos:   :ventura

  app "Nagare.app"

  # 卸载只做两件事：让正在跑的实例退出、删掉 .app。
  #
  # ⚠️ 【绝不能】在 uninstall 里删配置目录。升级、重装都会走 uninstall，
  # 而那个目录里是观看进度、媒体库状态、鉴权 token 与 animego 会话 ——
  # 在这里删就是静默丢数据，而且用户没要求过。清干净是 zap 的事，见下。
  uninstall quit: "io.github.nagare-project.Nagare"

  # zap 只在用户显式要求时才跑（brew uninstall --zap --cask nagare），
  # 语义就是「连数据一起清掉」，所以配置目录放在这里。
  # 注意：设了 NAGARE_CONFIG_DIR 的用户，配置不在下面这些路径里，zap 清不到。
  zap trash: [
    "~/Library/Application Support/nagare",
    "~/Library/Caches/io.github.nagare-project.Nagare",
    "~/Library/Preferences/io.github.nagare-project.Nagare.plist",
    "~/Library/Saved Application State/io.github.nagare-project.Nagare.savedState",
  ]

  caveats <<~EOS
    nagare 没有购买代码签名证书，Nagare.app 是 ad-hoc 签名的。
    brew 不会、也不能替你绕过 Gatekeeper —— 首次打开时系统仍然会拦一次：

      1. 打开「应用程序」里的 Nagare，出现「无法验证开发者」时点「完成」
      2. 打开 系统设置 › 隐私与安全性，在「安全性」一栏点「仍要打开」，再确认一次

    放行一次之后不再询问。始终看不到「仍要打开」时执行：

      xattr -c "#{appdir}/Nagare.app"

    mpv 已作为依赖一起装好，不用再单独装。
    nagare 是菜单栏应用（不占 Dock），启动后浏览器会自动打开界面。
  EOS
end
