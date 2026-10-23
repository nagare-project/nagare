# nagare Windows 桌面窗口测试（测试构建 -tags desktoptest）。
#
# 用户手里的测试包（双击 run-test.cmd）与 CI 的 Windows 冒烟 job 跑的都是这一份。全程：
#   - 用一个全新的临时配置目录（不碰你自己的 nagare 配置、不登录 animego）；
#   - 起 nagare → 加测试片目录 → 在窗口里播 → 逐项检查控制（暂停 / 右键菜单 / 全屏 / 最大化 / 最小化 / 置顶 /
#     下一集 / 返回媒体库 / 关窗口回写进度 / 退出）→ 每一步截图；
#   - 最后（可跳过）模拟「没有 WebView2」再起一次，看退回浏览器模式；
#   - 把截图、报告（report.json + report.txt）、日志（token 已抹掉）打成 nagare-wintest-report.zip。
# 只用 Windows PowerShell 5.1 也有的写法（Win10 自带的就是 5.1）。
[CmdletBinding()]
param(
    [string]$Exe = "",
    [string]$Media = "",
    [string]$OutDir = "",
    [switch]$CI,
    [switch]$SkipFallback,
    [switch]$KeepTemp,
    # 手动检查：起好 nagare（临时配置、测试片已加进媒体库），等你在窗口里自己试完按回车，再收日志
    [switch]$Manual,
    # 退路：不重新启用 mpv 的子窗口，输入由 nagare 经 IPC 转发（NAGARE_DESKTOP_INPUT=ipc）
    [switch]$IpcInput
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Off # 遥控的状态里有的字段只在播放时才有，读不到当空
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $Exe) { $Exe = Join-Path $here 'nagare\nagare.exe' }
if (-not $Media) { $Media = Join-Path $here 'media\clip.mkv' }
if (-not $OutDir) { $OutDir = [Environment]::GetFolderPath('Desktop') }

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$root = Join-Path $env:TEMP "nagare-wintest-$stamp"
$cfgDir = Join-Path $root 'config'
$mediaDir = Join-Path $root 'media'
$shots = Join-Path $root 'report\screenshots'
$reportDir = Join-Path $root 'report'
New-Item -ItemType Directory -Force -Path $cfgDir, $mediaDir, $shots | Out-Null

$steps = New-Object System.Collections.ArrayList
$script:proc = $null
$script:procs = New-Object System.Collections.ArrayList # 起过的每一个 nagare（收尾时一个都不能漏）
$script:startNo = 0
$script:job = $null
$script:api = $null
$script:ctl = $null

function Note([string]$msg) { Write-Host ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss'), $msg) }

function Add-Step([string]$name, $ok, [string]$detail, [string]$shot = '', $state = $null) {
    $o = [ordered]@{ name = $name; ok = $ok; detail = $detail; shot = $shot; time = (Get-Date -Format 's'); state = $state }
    [void]$steps.Add($o)
    $mark = if ($ok -eq $true) { 'OK  ' } elseif ($ok -eq $false) { 'FAIL' } else { 'INFO' }
    Note "$mark $name  $detail"
}

function Wait-Until([scriptblock]$cond, [int]$TimeoutSec = 15, [int]$IntervalMs = 250) {
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        try { if (& $cond) { return $true } } catch { }
        Start-Sleep -Milliseconds $IntervalMs
    }
    return $false
}

function Free-Port {
    $l = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 0)
    $l.Start(); $p = $l.LocalEndpoint.Port; $l.Stop(); return $p
}

# ── nagare 的 API（带 token）与测试遥控（带每次运行的密钥） ──

function Read-Config {
    $f = Join-Path $cfgDir 'config.toml'
    if (-not (Test-Path $f)) { return $null }
    $t = Get-Content -Raw -Encoding UTF8 $f
    $port = [regex]::Match($t, '(?m)^\s*port\s*=\s*(\d+)').Groups[1].Value
    $token = [regex]::Match($t, '(?m)^\s*token\s*=\s*"([^"]+)"').Groups[1].Value
    if (-not $port -or -not $token) { return $null }
    return @{ base = "http://127.0.0.1:$port"; token = $token }
}

function Api([string]$method, [string]$path, $body = $null) {
    $h = @{ 'X-Nagare-Token' = $script:api.token }
    $req = @{ Uri = $script:api.base + $path; Method = $method; Headers = $h; UseBasicParsing = $true; TimeoutSec = 30 }
    if ($null -ne $body) {
        $req.Body = [Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Compress -Depth 6))
        $req.ContentType = 'application/json; charset=utf-8'
    }
    $r = Invoke-WebRequest @req
    $text = [Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())
    return ($text | ConvertFrom-Json)
}

function Ctl([string]$path, [hashtable]$q = @{}) {
    $parts = @()
    foreach ($k in $q.Keys) { $parts += ('{0}={1}' -f $k, [uri]::EscapeDataString([string]$q[$k])) }
    $uri = $script:ctl.base + $path
    if ($parts.Count -gt 0) { $uri += '?' + ($parts -join '&') }
    $r = Invoke-WebRequest -Uri $uri -Headers @{ 'X-Nagare-Test-Secret' = $script:ctl.secret } -UseBasicParsing -TimeoutSec 30
    return [Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())
}

function State { return (Ctl '/state' | ConvertFrom-Json) }
function Prop($s, [string]$name) {
    if ($null -eq $s -or -not ($s.PSObject.Properties.Name -contains 'mpv')) { return $null }
    $m = $s.mpv
    if ($m.PSObject.Properties.Name -contains $name) { return $m.$name }
    return $null
}

$script:shotNo = 0
function Snap([string]$name, [switch]$Full) {
    $script:shotNo++
    $file = '{0:D2}-{1}.png' -f $script:shotNo, $name
    $q = @{ path = (Join-Path $shots $file) }
    if ($Full) { $q.full = '1' }
    try { Ctl '/snap' $q | Out-Null; return $file } catch { Note "截图失败：$($_.Exception.Message)"; return '' }
}

function Front { try { Ctl '/winop' @{ op = 'front' } | Out-Null; Start-Sleep -Milliseconds 300 } catch { } }
function Tap([int]$vk, [string]$mods = '') { Front; Ctl '/input' @{ kind = 'tap'; vk = $vk; mods = $mods } | Out-Null }
function Mouse([string]$kind, [int]$x, [int]$y, [string]$button = 'left') { Ctl '/input' @{ kind = $kind; x = $x; y = $y; button = $button } | Out-Null }
function Mpv([string]$json) { return (Ctl '/mpv' @{ cmd = $json } | ConvertFrom-Json) }
function Center($s) { return @([int]($s.client[0] / 2), [int]($s.client[1] / 2)) }

function Step([string]$name, [scriptblock]$body) {
    try { & $body } catch {
        $shot = Snap 'error' -Full
        Add-Step $name $false ("出错：" + $_.Exception.Message) $shot
    }
}

# ── 起 / 停 nagare ──

function Start-Nagare([hashtable]$extraEnv, [string[]]$argv) {
    $ctlPort = Free-Port
    $secret = Join-Path $root 'testctl.secret'
    Remove-Item -Force -ErrorAction SilentlyContinue $secret
    $env:NAGARE_CONFIG_DIR = $cfgDir
    $env:NAGARE_DESKTOP_TEST_ADDR = "127.0.0.1:$ctlPort"
    $env:NAGARE_DESKTOP_TEST_SECRET_FILE = $secret
    $env:NAGARE_DESKTOP_TEST_NO_WEBVIEW2 = ''
    $env:NAGARE_DESKTOP_INPUT = ''
    if ($IpcInput) { $env:NAGARE_DESKTOP_INPUT = 'ipc' }
    # 测试用不着磁力：不起 BT 引擎（它会在所有网卡上监听、向路由器要 UPnP 映射，Windows 会弹防火墙对话框抢焦点）
    $env:NAGARE_TEST_NO_TORRENT = '1'
    foreach ($k in $extraEnv.Keys) { Set-Item -Path "env:$k" -Value $extraEnv[$k] }
    $script:startNo++
    $sp = @{ FilePath = $Exe; PassThru = $true
        RedirectStandardError = (Join-Path $reportDir ("stderr-{0}.txt" -f $script:startNo))
        RedirectStandardOutput = (Join-Path $reportDir ("stdout-{0}.txt" -f $script:startNo)) }
    if ($argv -and $argv.Count -gt 0) { $sp.ArgumentList = $argv }
    $script:proc = Start-Process @sp
    [void]$script:procs.Add($script:proc)
    Join-KillJob $script:proc
    $ok = Wait-Until { Test-Path $secret } 30
    if (-not $ok) { throw "测试遥控没有起来（30 秒）：可能不是 desktoptest 构建，或 nagare 起不来" }
    $script:ctl = @{ base = "http://127.0.0.1:$ctlPort"; secret = (Get-Content -Raw $secret).Trim() }
    $ok = Wait-Until { $c = Read-Config; if ($null -eq $c) { return $false }; $script:api = $c; (Api 'GET' '/api/settings').success } 30
    if (-not $ok) { throw "nagare 的 API 30 秒内没起来" }
}

# 起来的 nagare 放进一个作业对象（KILL_ON_JOB_CLOSE，句柄归这个 PowerShell 进程）：测试中途关掉黑色窗口时，
# PowerShell 一退，nagare 连同它的 mpv 一起被系统结束，不会在后台留下一个用临时配置跑着的 nagare。
function Join-KillJob($proc) {
    try {
        if ($null -eq $script:job) {
            if (-not ('NagareKitJob' -as [type])) {
                Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class NagareKitJob {
    [StructLayout(LayoutKind.Sequential)] struct Basic { public long a, b; public uint Flags; public UIntPtr c, d; public uint e; public UIntPtr f; public uint g, h; }
    [StructLayout(LayoutKind.Sequential)] struct Io { public ulong a, b, c, d, e, f; }
    [StructLayout(LayoutKind.Sequential)] struct Ext { public Basic B; public Io I; public UIntPtr p, j, pp, jp; }
    [DllImport("kernel32.dll", SetLastError = true)] static extern IntPtr CreateJobObject(IntPtr a, string n);
    [DllImport("kernel32.dll", SetLastError = true)] static extern bool SetInformationJobObject(IntPtr j, int c, ref Ext i, int n);
    [DllImport("kernel32.dll", SetLastError = true)] static extern bool AssignProcessToJobObject(IntPtr j, IntPtr p);
    public static IntPtr Create() {
        IntPtr j = CreateJobObject(IntPtr.Zero, null);
        Ext e = new Ext(); e.B.Flags = 0x2000; // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
        if (j == IntPtr.Zero || !SetInformationJobObject(j, 9, ref e, Marshal.SizeOf(typeof(Ext)))) return IntPtr.Zero;
        return j;
    }
    public static bool Assign(IntPtr j, IntPtr p) { return AssignProcessToJobObject(j, p); }
}
'@ -IgnoreWarnings
            }
            $script:job = [NagareKitJob]::Create()
        }
        if ($script:job -ne [IntPtr]::Zero) { [void][NagareKitJob]::Assign($script:job, $proc.Handle) }
    } catch { Note "没能把 nagare 放进作业对象（中途关窗口时它可能留在后台）：$($_.Exception.Message)" }
}

# 收尾：每一个还活着的 nagare 先请它退出（会停播、回写进度），15 秒还不走就连进程树一起结束。
function Stop-AllNagare {
    $alive = @($script:procs | Where-Object { $null -ne $_ -and -not $_.HasExited })
    if ($alive.Count -eq 0) { return }
    try { Api 'POST' '/api/shutdown' | Out-Null } catch { }
    foreach ($p in $alive) {
        try { [void]$p.WaitForExit(15000) } catch { }
        if (-not $p.HasExited) {
            Note "nagare（PID $($p.Id)）15 秒内没退出，连同子进程一起结束"
            & taskkill.exe /T /F /PID $p.Id 2>&1 | Out-Null
        }
    }
}

function Stop-Nagare([int]$TimeoutSec = 40) {
    if ($null -eq $script:proc) { return $true }
    try { Api 'POST' '/api/shutdown' | Out-Null } catch { }
    $exited = $script:proc.WaitForExit($TimeoutSec * 1000)
    if (-not $exited) { & taskkill.exe /T /F /PID $script:proc.Id 2>&1 | Out-Null }
    return $exited
}

function Progress([string]$fileId) {
    $f = Join-Path $cfgDir 'state.json'
    if (-not (Test-Path $f)) { return $null }
    $s = Get-Content -Raw -Encoding UTF8 $f | ConvertFrom-Json
    if ($null -eq $s.progress) { return $null }
    $p = $s.progress.PSObject.Properties | Where-Object { $_.Name -eq $fileId } | Select-Object -First 1
    if ($null -eq $p) { return $null }
    return $p.Value
}

# ── 环境 ──

function Collect-Env {
    $os = Get-CimInstance Win32_OperatingSystem
    $gpu = (Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name }) -join '; '
    $pv = @()
    foreach ($k in @('HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
                     'HKCU:\Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}')) {
        try { $pv += ('{0} = {1}' -f $k, (Get-ItemProperty -Path $k -Name pv -ErrorAction Stop).pv) } catch { $pv += "$k = (无)" }
    }
    $others = @(Get-Process -Name nagare -ErrorAction SilentlyContinue)
    return [ordered]@{
        os = "$($os.Caption) $($os.Version) build $($os.BuildNumber)"
        powershell = $PSVersionTable.PSVersion.ToString()
        gpu = $gpu
        webview2Registry = $pv
        otherNagareRunning = $others.Count
        exe = $Exe
        ci = [bool]$CI
    }
}

# ══════════════════════════════ 开始 ══════════════════════════════

$exitCode = 1
$zipName = 'nagare-wintest-report.zip'
if ($Manual) { $zipName = 'nagare-wintest-manual.zip' }
if ($IpcInput) { $zipName = $zipName.Replace('.zip', '-ipc-input.zip') }
$summary = '没跑完'
try {
Note "测试目录：$root"
if (-not (Test-Path $Exe)) { throw "找不到 $Exe（把整个测试包解压后再运行）" }
if (-not (Test-Path $Media)) { throw "找不到测试片 $Media" }
# 解压出来的文件带「来自网络」标记，不去掉的话 SmartScreen 会拦 nagare.exe、mpv.exe 与 WebView2 的安装器
Get-ChildItem -Recurse -File (Split-Path -Parent $Exe) | Unblock-File -ErrorAction SilentlyContinue
Get-ChildItem -File $here | Unblock-File -ErrorAction SilentlyContinue
$envInfo = Collect-Env
if ($envInfo.otherNagareRunning -gt 0 -and -not $CI) {
    Write-Host '检测到另一个 nagare 在运行：请先从托盘退出它（右键托盘图标 → 退出 nagare），然后按回车继续。' -ForegroundColor Yellow
    [void](Read-Host)
}
# ── WebView2 运行时：没有就用测试包里的 bootstrapper 装（与安装包内嵌的是同一份、同一条命令） ──
function Has-Runtime($info) {
    return (@($info.webview2Registry | Where-Object { $_ -match '= \d+\.\d+\.\d+\.\d+$' -and $_ -notmatch '= 0\.0\.0\.0$' }).Count -gt 0)
}
$wv2 = Join-Path $here 'MicrosoftEdgeWebview2Setup.exe'
if (Has-Runtime $envInfo) {
    Add-Step 'WebView2 运行时已安装' $null (($envInfo.webview2Registry) -join '；') ''
} else {
    Add-Step 'WebView2 运行时' $null '这台电脑上没有 WebView2 运行时（注册表两处都没有）' ''
    if (-not $CI -and (Test-Path $wv2)) {
        Write-Host '这台电脑没有 WebView2 运行时（nagare 的窗口要用）。要用测试包里的微软官方安装器静默安装吗（要联网，约 1–2 分钟）？输入 y 回车安装；直接回车跳过。' -ForegroundColor Yellow
        $answer = Read-Host
        if ($answer -eq 'y' -or $answer -eq 'Y') {
            $p = Start-Process -FilePath $wv2 -ArgumentList '/silent', '/install' -Wait -PassThru
            $envInfo = Collect-Env
            Add-Step '用内嵌的 bootstrapper 静默安装 WebView2 运行时（/silent /install）' (($p.ExitCode -eq 0) -and (Has-Runtime $envInfo)) ("退出码 $($p.ExitCode)；" + (($envInfo.webview2Registry) -join '；')) ''
        }
    }
}

$names = @('[nagare-test] Nagare Windows 测试 - 01 [1080p].mkv', '[nagare-test] Nagare Windows 测试 - 02 [1080p].mkv', '[nagare-test] Nagare Windows 测试 - 03 [1080p].mkv')
foreach ($n in $names) { Copy-Item $Media (Join-Path $mediaDir $n) }

$ep = @{}
if ($Manual) {
    Step '手动检查' {
        Start-Nagare @{} @()
        Wait-Until { (State).webReady } 40 | Out-Null
        Api 'POST' '/api/library/folders' @{ path = $mediaDir } | Out-Null
        Ctl '/js' @{ code = 'location.reload(); true' } | Out-Null
        Write-Host ''
        Write-Host 'nagare 已经起来了（临时配置，测试片已在媒体库里）。请照 README-测试说明.txt 的「手动检查」逐项试，' -ForegroundColor Green
        Write-Host '把结果写进测试包里的「手动检查记录.txt」并保存。全部试完后回到这个窗口按回车。' -ForegroundColor Green
        [void](Read-Host)
        Add-Step '手动检查结束' $null '' (Snap 'manual-end' -Full) (State)
        [void](Stop-Nagare 40)
        $notes = Join-Path $here '手动检查记录.txt'
        if (Test-Path $notes) { Copy-Item $notes (Join-Path $reportDir 'manual-notes.txt') }
    }
} else {
Step '启动' {
    Start-Nagare @{} @()
    $ok = Wait-Until { $s = State; $s.visible -and $s.webReady } 40
    # 等网页真的画出来（首屏是 SPA，网页层就绪之后还要一两秒）
    $script:hintText = ''
    [void](Wait-Until { $script:hintText = (Ctl '/js' @{ code = "(document.querySelector('.background-hint-text') || {}).textContent || ''" }).Trim().Trim('"'); $script:hintText -ne '' } 15)
    Start-Sleep -Seconds 1
    $s = State
    $shot = Snap 'startup'
    Add-Step '启动：窗口显示、网页层（WebView2）就绪' ($ok -and $s.active) ("WebView2 $($s.runtime)；窗口 $($s.client[0])x$($s.client[1])，DPI $($s.dpi)；fallback=$($s.fallback)") $shot $s
    $ua = (Ctl '/js' @{ code = 'navigator.userAgent' }).Trim()
    Add-Step '网页层的 User-Agent 末尾带 NagareDesktop（网页据此说「关闭窗口」）' ($ua -match ' NagareDesktop"?$') $ua ''
    $hint = $script:hintText
    $settings = (Api 'GET' '/api/settings').data
    Add-Step '后台形态是 window，提示条说「关闭窗口」' (($settings.background.mode -eq 'window') -and ($hint -match '关闭窗口')) ("mode=$($settings.background.mode)；提示条：$hint") ''
}

Step '媒体库' {
    $r = Api 'POST' '/api/library/folders' @{ path = $mediaDir }
    $addInfo = ($r | ConvertTo-Json -Compress -Depth 8)
    $lib = (Api 'GET' '/api/library').data
    foreach ($c in @($lib.clusters)) { foreach ($g in @($c.groups)) { foreach ($it in @($g.items)) { $ep[[int]$it.episode] = $it.fileId } } }
    Ctl '/js' @{ code = 'location.reload(); true' } | Out-Null
    Start-Sleep -Seconds 3
    $shot = Snap 'library'
    $detail = "认出 $($ep.Count) 集"
    if ($ep.Count -lt 3) { $detail += "；加目录的回复：$addInfo" }
    Add-Step '加测试片目录' ($ep.Count -ge 3) $detail $shot
}

Step '播放' {
    Api 'POST' '/api/play' @{ fileId = $ep[1] } | Out-Null
    $ok = Wait-Until { $s = State; $s.mode -eq 'video' -and $s.child -and $s.ipcConnected } 30
    Start-Sleep -Seconds 2
    Front
    $s = State
    $shot = Snap 'playing'
    Add-Step '在窗口里播放（mpv 挂进画面层）' $ok ("子窗口 enabled=$($s.childEnabled)、焦点在 mpv=$($s.focusIsChild)、前台=$($s.foreground)、mpv pid $($s.mpvPid)") $shot $s
    if ($IpcInput) {
        Add-Step 'IPC 转发模式：mpv 的子窗口保持 disabled，输入落在 nagare 的画面层' ((-not $s.childEnabled) -and $s.ipcInput) '' '' $null
    } else {
        Add-Step 'mpv 的子窗口已重新启用（--wid 下默认是 disabled）' ([bool]$s.childEnabled) '' '' $null
    }
}

Step '控制条' {
    $s = State; $c = Center $s
    Front
    Mouse 'move' ($c[0] - 40) ($c[1] - 20); Start-Sleep -Milliseconds 200; Mouse 'move' $c[0] $c[1]
    $ok = Wait-Until { (Prop (State) 'user-data/nagare/shown') -eq $true } 5
    $shot = Snap 'controls'
    Add-Step '鼠标移到画面上，控制条出现（真实鼠标进得了 mpv 窗口）' $ok '' $shot
}

Step '暂停' {
    Mpv '["set_property","pause",true]' | Out-Null
    $ok1 = Wait-Until { (Prop (State) 'pause') -eq $true } 5
    $shot = Snap 'paused-ipc'
    Add-Step '经 mpv IPC 暂停' $ok1 '' $shot
    Tap 0x20
    $ok2 = Wait-Until { (Prop (State) 'pause') -eq $false } 5
    Add-Step '按空格继续播放（真实键盘进得了 mpv 窗口）' $ok2 '' ''
}

Step '右键菜单' {
    $s = State; $c = Center $s
    Front
    Mouse 'click' $c[0] $c[1] 'right'
    $ok = Wait-Until { (Prop (State) 'user-data/nagare/capture') -eq $true } 5
    Start-Sleep -Milliseconds 400
    $shot = Snap 'context-menu'
    Add-Step '右键菜单打开' $ok '' $shot
    Tap 0x1B
    Start-Sleep -Milliseconds 600
    $s = State
    Add-Step 'Esc 只关菜单，不返回媒体库' ($s.mode -eq 'video') ("mode=$($s.mode)") ''
}

Step '单按 Alt' {
    # 单按一下 Alt：Windows 会让窗口进「菜单模式」。mpv 的窗口把它转给 nagare 的窗口时如果被挡住，mpv 的窗口线程
    # 会卡住（画面冻住、看门狗会把 mpv 结束掉）。按完 Alt 之后空格必须照常管用，8 秒后 mpv 必须还活着、没卡。
    Front
    Mpv '["set_property","pause",false]' | Out-Null
    Ctl '/input' @{ kind = 'tap'; vk = 0x12 } | Out-Null
    Start-Sleep -Milliseconds 300
    Ctl '/input' @{ kind = 'tap'; vk = 0x20 } | Out-Null
    $ok = Wait-Until { (Prop (State) 'pause') -eq $true } 5
    Start-Sleep -Seconds 8
    $s = State
    Add-Step '单按 Alt 之后：空格照常暂停，mpv 的窗口没卡住、没被看门狗结束' ($ok -and $s.mode -eq 'video' -and -not $s.childHung -and -not $s.killedByWatchdog) ("childHung=$($s.childHung) killed=$($s.killedByWatchdog)") (Snap 'after-alt')
    Mpv '["set_property","pause",false]' | Out-Null
}

Step '全屏' {
    $s = State; $c = Center $s
    Front
    Mouse 'dblclick' $c[0] $c[1]
    $ok = Wait-Until { (State).fullscreen } 5
    Start-Sleep -Seconds 1
    $shot = Snap 'fullscreen-dblclick' -Full
    $s = State
    Add-Step '双击画面进全屏（宿主窗口全屏、mpv 的属性跟着）' $ok ("mpv fullscreen=$(Prop $s 'fullscreen')") $shot $s
    Tap 0x1B
    $ok2 = Wait-Until { -not (State).fullscreen } 5
    Start-Sleep -Milliseconds 500
    $s = State
    Add-Step 'Esc 退出全屏，仍在播放' ($ok2 -and $s.mode -eq 'video') ("mode=$($s.mode)") (Snap 'fullscreen-exit')
    Tap 0x0D
    $ok3 = Wait-Until { (State).fullscreen } 5
    Mpv '["set_property","fullscreen",false]' | Out-Null
    $ok4 = Wait-Until { -not (State).fullscreen } 5
    Add-Step 'Enter 进全屏、mpv 改 fullscreen=no 退出（属性→窗口）' ($ok3 -and $ok4) '' ''
}

Step '最大化' {
    Ctl '/winop' @{ op = 'maximize' } | Out-Null
    $ok = Wait-Until { $s = State; $s.maximized -and (Prop $s 'window-maximized') -eq $true } 5
    $shot = Snap 'maximized' -Full
    Add-Step '窗口最大化，写回 mpv 的 window-maximized' $ok '' $shot
    Ctl '/winop' @{ op = 'restore' } | Out-Null
    $ok2 = Wait-Until { -not (State).maximized } 5
    Mpv '["set_property","window-maximized",true]' | Out-Null
    $ok3 = Wait-Until { (State).maximized } 5
    Ctl '/winop' @{ op = 'restore' } | Out-Null
    Wait-Until { -not (State).maximized } 5 | Out-Null
    Add-Step '还原；mpv 请求最大化落到窗口上' ($ok2 -and $ok3) '' ''
}

Step '最小化' {
    Mpv '["set_property","window-minimized",true]' | Out-Null
    $ok = Wait-Until { (State).minimized } 5
    $shot = Snap 'minimized' -Full
    Ctl '/winop' @{ op = 'restore' } | Out-Null
    $ok2 = Wait-Until { $s = State; -not $s.minimized -and $s.visible } 5
    Add-Step 'mpv 请求最小化 → 窗口最小化，再还原' ($ok -and $ok2) '' $shot
}

Step '置顶' {
    Tap 0x54
    $ok = Wait-Until { (State).topmost } 5
    Tap 0x54
    $ok2 = Wait-Until { -not (State).topmost } 5
    Add-Step '按 t 置顶 / 取消置顶' ($ok -and $ok2) '' ''
}

Step '下一集' {
    Tap 0x4E 'shift'
    $ok = Wait-Until { (Api 'GET' '/api/player/status').data.fileId -eq $ep[2] } 20
    Start-Sleep -Seconds 2
    $s = State
    $shot = Snap 'next-episode'
    Add-Step 'Shift+N 在同一个窗口里切到第 2 集' ($ok -and $s.mode -eq 'video') ("标题：$($s.title)") $shot $s
}

Step '返回' {
    Start-Sleep -Seconds 3
    Tap 0x1B
    $ok = Wait-Until { $s = State; $s.mode -eq 'web' -and $s.sessionId -eq 0 } 10
    Start-Sleep -Seconds 2
    $p = Progress $ep[2]
    $shot = Snap 'back-to-library'
    $pos = if ($null -ne $p) { $p.positionSec } else { 0 }
    Add-Step 'Esc 返回媒体库（换回网页层）' $ok '' $shot
    Add-Step '返回后第 2 集的进度已写下' ($pos -gt 0) ("positionSec=$pos") ''
}

Step '关窗口' {
    Api 'POST' '/api/play' @{ fileId = $ep[1] } | Out-Null
    $ok = Wait-Until { $s = State; $s.mode -eq 'video' -and $s.child } 30
    Mpv '["set_property","pause",false]' | Out-Null
    Start-Sleep -Seconds 7 # 进度要过 5 秒才记（更短的当作没看）
    $before = Progress $ep[1]
    $t0 = if ($null -ne $before) { $before.updatedAt } else { 0 }
    Ctl '/winop' @{ op = 'close' } | Out-Null
    $ok2 = Wait-Until { $s = State; -not $s.visible -and $s.mode -eq 'web' } 15
    $ok3 = Wait-Until { $p = Progress $ep[1]; $null -ne $p -and $p.updatedAt -gt $t0 } 10
    $shot = Snap 'after-close' -Full
    $st = (Api 'GET' '/api/player/status').data
    Add-Step '播放中关窗口：停播、窗口藏起来、nagare 还在' ($ok -and $ok2 -and -not $st.playing) '' $shot
    Add-Step '关窗口时进度已回写' $ok3 '' ''
    Ctl '/show' | Out-Null
    $ok4 = Wait-Until { $s = State; $s.visible -and $s.mode -eq 'web' } 10
    Start-Sleep -Seconds 1
    Add-Step '托盘「打开 nagare」（Show）重新显示窗口' $ok4 '' (Snap 'reopened')
}

Step '第二次启动' {
    # nagare 开着时再启动一次（同一个配置目录）：新进程应该把已有的窗口叫到前面然后自己退出。
    # 新进程不能带测试遥控的环境变量：它会重写密钥文件、抢同一个端口
    $s0 = State
    Ctl '/winop' @{ op = 'minimize' } | Out-Null
    Start-Sleep -Milliseconds 500
    $saved = @{ a = $env:NAGARE_DESKTOP_TEST_ADDR; f = $env:NAGARE_DESKTOP_TEST_SECRET_FILE }
    $env:NAGARE_DESKTOP_TEST_ADDR = ''; $env:NAGARE_DESKTOP_TEST_SECRET_FILE = ''
    $second = Start-Process -FilePath $Exe -PassThru
    $env:NAGARE_DESKTOP_TEST_ADDR = $saved.a; $env:NAGARE_DESKTOP_TEST_SECRET_FILE = $saved.f
    $exited = $second.WaitForExit(20000)
    Start-Sleep -Seconds 2
    $s = State
    $shot = Snap 'second-launch' -Full
    Add-Step '第二次启动：新进程自己退出' $exited ("退出码 $(if ($exited) { $second.ExitCode } else { '没退出' })") $shot
    $edge = @(Get-Process -Name msedge -ErrorAction SilentlyContinue).Count
    Add-Step '第二次启动：已有的窗口从最小化恢复、到前面（不开浏览器）' ($s.visible -and -not $s.minimized) ("visible=$($s.visible) minimized=$($s.minimized) foreground=$($s.foreground)，Edge 进程 $edge 个") ''
    Add-Step '第二次启动：窗口拿到了前台（AllowSetForegroundWindow 让出前台）' $null ("foreground=$($s.foreground)") ''
    if (-not $exited) { try { $second.Kill() } catch { } }
    # 万一退回了开浏览器（旧行为），CI 上把它关掉，免得挡住后面的截图
    if ($CI) { Get-Process -Name msedge -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue }
}

Step '退出' {
    Api 'POST' '/api/player/stop' | Out-Null
    $exited = Stop-Nagare 40
    $log = Join-Path $cfgDir 'logs\nagare.log'
    $clean = (Test-Path $log) -and ((Get-Content -Raw -Encoding UTF8 $log) -match '已退出')
    Add-Step '退出 nagare（收尾完成后进程结束）' ($exited -and $clean) ("进程退出=$exited，日志里有「已退出」=$clean") ''
}

if (-not $SkipFallback) {
    Step '没有 WebView2' {
        Start-Nagare @{ NAGARE_DESKTOP_TEST_NO_WEBVIEW2 = '1' } @('-no-browser')
        $st = (Api 'GET' '/api/settings').data
        $s = State
        Add-Step '模拟没有 WebView2：退回浏览器模式，设置页写着原因' ((-not $st.desktop.window) -and $st.desktop.fallback) ("fallback：$($st.desktop.fallback)") '' $s
        Api 'POST' '/api/play' @{ fileId = $ep[3] } | Out-Null
        $ok = Wait-Until { (Api 'GET' '/api/player/status').data.playing } 20
        Start-Sleep -Seconds 3
        $shot = Snap 'fallback-separate-mpv' -Full
        $s = State
        Add-Step '退回模式下 mpv 单独开窗口播放' ($ok -and $s.mode -ne 'video') ("mode=$($s.mode)") $shot
        Api 'POST' '/api/player/stop' | Out-Null
        [void](Stop-Nagare 40)
    }
}

} # 自动检查到此为止（-Manual 时跳过）

# ── 报告 ──

$failed = @($steps | Where-Object { $_.ok -eq $false }).Count
$passed = @($steps | Where-Object { $_.ok -eq $true }).Count
$report = [ordered]@{ summary = "通过 $passed 项，失败 $failed 项"; env = $envInfo; steps = $steps; when = (Get-Date -Format 's') }
$report | ConvertTo-Json -Depth 8 | Set-Content -Encoding UTF8 (Join-Path $reportDir 'report.json')
$lines = @("nagare Windows 桌面窗口测试  $(Get-Date)", $report.summary, '', '环境：')
foreach ($k in $envInfo.Keys) { $lines += ('  {0}: {1}' -f $k, ($envInfo[$k] -join ' | ')) }
$lines += ''
foreach ($st in $steps) {
    $mark = if ($st.ok -eq $true) { '[通过]' } elseif ($st.ok -eq $false) { '[失败]' } else { '[信息]' }
    $lines += ('{0} {1}  {2}  {3}' -f $mark, $st.name, $st.detail, $st.shot)
}
$lines | Set-Content -Encoding UTF8 (Join-Path $reportDir 'report.txt')

# 日志与 nagare 的标准输出 / 错误：抹掉 token 与任何 32 位以上的十六进制串（能力段、token 都是这个形状）
function Scrub([string]$text) {
    if ($null -eq $text) { return $null }
    if ($script:api -and $script:api.token) { $text = $text.Replace($script:api.token, '<token>') }
    return [regex]::Replace($text, '\b[0-9a-fA-F]{32,}\b', '<hex>')
}
Stop-AllNagare # 标准输出 / 错误的文件 nagare 还开着时读不全
$logDir = Join-Path $cfgDir 'logs'
if (Test-Path $logDir) {
    foreach ($f in Get-ChildItem $logDir -File) {
        $t = Scrub (Get-Content -Raw -Encoding UTF8 $f.FullName)
        if ($null -eq $t) { continue }
        Set-Content -Encoding UTF8 -Path (Join-Path $reportDir ('log-' + $f.Name)) -Value $t
    }
}
foreach ($f in Get-ChildItem $reportDir -File -Filter 'std*-*.txt') {
    $t = Scrub (Get-Content -Raw -Encoding UTF8 $f.FullName)
    if ($null -eq $t) { $t = '' }
    Set-Content -Encoding UTF8 -Path $f.FullName -Value $t
}
$zip = Join-Path $OutDir $zipName
Remove-Item -Force -ErrorAction SilentlyContinue $zip
Compress-Archive -Path (Join-Path $reportDir '*') -DestinationPath $zip
$summary = $report.summary
Note "报告：$zip（$summary）"
if ($failed -eq 0) { $exitCode = 0 }
} finally {
    # 不管跑到哪一步、出了什么错（包括 Ctrl+C），都把起过的 nagare 收掉、删掉临时目录
    Stop-AllNagare
    if (-not $KeepTemp) { Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $root }
}
if (-not $CI) {
    Write-Host ''
    Write-Host "跑完了：$summary。请把桌面上的 $zipName 发回来。" -ForegroundColor Green
    if (-not $Manual) { Write-Host '接着请双击 run-manual.cmd 做「手动检查」（见 README-测试说明.txt）。' }
    Write-Host '按回车关闭这个窗口。'
    [void](Read-Host)
}
exit $exitCode
