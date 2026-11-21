# nagare: Additional Permission for Apple App Store Distribution

> **DRAFT — NOT IN EFFECT.** This text has not yet been finalized by the copyright
> holder. It takes effect only when this notice is removed and the placeholder
> `[COPYRIGHT HOLDER]` below is replaced with the copyright holder's full legal name.

Copyright (C) 2026 [COPYRIGHT HOLDER]

nagare is free software: you can redistribute it and/or modify it under the terms
of the GNU Affero General Public License version 3 as published by the Free Software
Foundation (the "License"; see the file [`LICENSE`](LICENSE)), supplemented by the
additional permission below.

## Additional permission under GNU AGPL version 3 section 7

### 1. Definitions

"Program" means nagare, the software published in this repository, in any version
that is accompanied by this permission.

"Copyright Holder" means [COPYRIGHT HOLDER].

"Covered Material" means those parts of the Program (a) in which the Copyright Holder
holds the copyright, or (b) which a contributor has licensed to the Copyright Holder
under the nagare Contributor License Agreement ([`CLA.md`](CLA.md)) or under other
written terms that allow the Copyright Holder to grant this permission for them.

"Third-Party Component" means any part of the Program, and any software distributed
with the Program or linked into it, that is not Covered Material, including the
components listed in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) (for example
mpv and libmpv, FFmpeg, and their dependencies).

"Apple Distribution Service" means an application distribution service operated by
Apple Inc. or its affiliates, including the App Store for any Apple platform and
TestFlight.

"Apple Terms" means the terms of service, usage rules, end-user license agreements,
developer agreements and guidelines that Apple applies to software distributed
through an Apple Distribution Service, and the technical measures Apple applies to
such software, such as code signing, encryption and digital rights management.

### 2. Permission

As an additional permission under section 7 of the License, the Copyright Holder
grants you permission to convey the Program, or a work based on the Program, in
object code form through an Apple Distribution Service, even though the Apple Terms
impose on you or on recipients restrictions or requirements that the License would
otherwise not permit, provided that you meet the conditions in section 3.

This permission applies only to the extent that the Apple Terms conflict with the
License, including its sections 6 and 10. It does not relieve you of any other
obligation under the License.

### 3. Conditions

(a) **Corresponding Source.** For each version you convey under this permission, you
make the Corresponding Source of that version, as defined in the License, available
to anyone free of charge, under the terms of the License (with or without this
permission), through a channel that is not subject to the Apple Terms, such as a
publicly accessible source code repository, for as long as you offer that version
through an Apple Distribution Service.

(b) **Notices in the application.** The version you convey shows, in a place within
the application that users can readily reach (such as an "About" or "Licenses"
screen): (i) a statement that it is licensed under the GNU Affero General Public
License version 3 with this additional permission; (ii) the text of the License and
of this permission, or a link to them; and (iii) where its Corresponding Source can
be obtained.

(c) **No restrictions of your own.** You do not impose on recipients any restriction
beyond those that the Apple Terms themselves require.

(d) **Everything else.** You comply with the License in all other respects, and with
the license of every Third-Party Component you convey.

### 4. Scope

(a) This permission is given only for Covered Material. It grants no permission for
any Third-Party Component, which remains subject solely to its own license. Before
relying on this permission, you are responsible for making sure that every
Third-Party Component you convey may itself be conveyed through an Apple
Distribution Service. In particular, components licensed under the GNU General
Public License, such as a build of libmpv configured with GPL-licensed parts, are
not covered by this permission.

(b) This permission does not apply to distribution through any channel other than an
Apple Distribution Service.

(c) This permission does not grant any right to use the name "nagare", its logos or
other marks.

(d) Except as expressly stated in this permission, the License applies unchanged.

### 5. Modified versions

If you modify the Program, you may extend this permission to your version, but you
are not obligated to do so, and you may extend it only to material that you have the
right to license in this way. If you do not wish to extend it, delete this permission
from your version. As provided in section 7 of the License, when you convey a copy of
the Program you may remove this permission from that copy, or from any part of it.

### 6. Changes

The Copyright Holder may change or withdraw this permission for future versions of
the Program. A change or withdrawal does not affect any version of the Program that
was conveyed with this permission, which continues to carry it.

### 7. Language

The Chinese explanation below is provided for convenience only. If it differs from
the English text above, the English text governs.

Apple, App Store and TestFlight are trademarks of Apple Inc., registered in the U.S.
and other countries.

---

## 中文说明（仅供参考，以上方英文为准）

**这是什么。** nagare 以 AGPL-3.0 发布（[`LICENSE`](LICENSE) 原文不变）。本文件是版权人依 AGPL-3.0 第 7 条
额外给出的一项**附加许可**：允许把 nagare（包括改过的版本）的可执行版本经 Apple 的分发渠道
（App Store、TestFlight）发布，哪怕 Apple 的条款与 AGPL 冲突。

**为什么需要。** App Store 的标准最终用户许可协议会限制安装设备、禁止用户再分发，并对 App 做签名与加密；
AGPL 第 10 条不允许在许可证之外再对接收者加限制。GPL 家族的软件因此与 App Store 条款冲突，
2011 年 VLC 就是因为版权人之一投诉而从 App Store 下架。只有版权人能给出这种例外，所以要写成附加许可。

**它不改变什么。**

- 对所有其他用途，nagare 仍然只是 AGPL-3.0；这项许可只放开「Apple 条款与 AGPL 冲突」的那一部分。
- 经 App Store 发布的每一个版本，都必须在不受 Apple 条款约束的地方（例如公开的源码仓库）免费提供完整对应源码，
  并在 App 内的「关于 / 许可」页写明 AGPL-3.0、这项附加许可和源码地址。不能借此闭源。
- **只覆盖版权人自己的代码**，以及贡献者按 [`CLA.md`](CLA.md) 授权过的代码。
  [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) 里列出的第三方组件（mpv / libmpv、FFmpeg 及其依赖、
  thumbfast、Anime4K、WebView2 绑定、各类 Go 与前端依赖、字体等）**不在范围内**，照旧适用各自的许可证。
  App Store 版里 libmpv / FFmpeg 的合规要另外按它们自己的许可证（LGPL 构建，不含 GPL 组件）处理，不靠这份例外。
- 不授予「nagare」名称或图标的使用权。

**派生版本。** fork 的人可以把这项许可延续到自己的版本，也可以删掉；删掉之后，那个版本就只是 AGPL-3.0。

**写法来源。** 结构参照 GPL 的链接例外（GNU Classpath Exception 等「可延续、可删除」的措辞，
以及自由软件基金会给出的第 7 条附加许可模板），App Store 部分参照 Mosh 的 `COPYING.iOS`
与 Signal 早期 iOS 版的 App Store 附加许可。
