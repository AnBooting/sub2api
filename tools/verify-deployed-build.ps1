#Requires -Version 7.0
<#
.SYNOPSIS
    校验已部署实例的后端版本与前端构建产物，并和本地 git HEAD / 本地 dist 比对。

.DESCRIPTION
    针对"看不出线上前端到底部署没"的运维盲区。回答四个问题：
      1. 线上后端跑的是哪个提交？（/api/v1/settings/public 的 version 字段）
      2. 线上前端跑的是哪次构建？（/ 返回的 HTML 里的 /assets/index-<hash>.js）
      3. HTML 内联的 window.__APP_CONFIG__.version 是什么？（与 1 应当一致）
      4. 本地 HEAD 是什么？本地 dist 又是哪一次构建？

    输出四行结论：线上后端 / 线上前端 / 内联版本 / 本地 HEAD，
    并按【强判据】与【弱判据】两级裁定是否通过：

      强判据（唯一决定退出码）：线上 version 解析出的 commit == 本地 git HEAD。
        版本号由构建期 ldflags 注入，与提交一一对应，是"线上跑的是不是这个提交"
        的可信证据。

      弱判据（仅供参考，默认不影响退出码）：线上入口 hash == 本地 dist 入口 hash。
        vite 产物 hash 受构建环境影响（node/pnpm 版本、模块排序、时间戳注入），
        同一提交在同机重建也可能得到不同 hash（实测已发生）。所以 hash 不一致
        只能说明"不是同一次构建"，【不能】说明"线上构建自不同提交"。
        需要严格执行时加 -StrictDistHash。

.PARAMETER BaseUrl
    被校验站点根地址，默认线上 https://sub2.lansn.icu。

.PARAMETER RepoRoot
    仓库根目录（含 .git）。默认按脚本位置推导：tools/ 的上一级。

.PARAMETER LocalDistRoot
    本地前端构建产物目录。默认 <RepoRoot>/backend/internal/web/dist。
    单独暴露是为了在不重新构建的前提下模拟"本地 dist 是新的"这一分支。

.PARAMETER TimeoutSec
    单次 HTTP 超时秒数，默认 30。

.EXAMPLE
    pwsh -File tools/verify-deployed-build.ps1
    pwsh -File tools/verify-deployed-build.ps1 -BaseUrl https://sub2.lansn.icu

.NOTES
    退出码（对外契约，勿改）：
      0 = 强判据通过（默认模式下弱判据不一致也只是 [WARN]，不影响退出码）
      1 = 强判据失败（输出里有 [DRIFT] 行）；-StrictDistHash 下弱判据失败也算
      2 = 取数失败（网络 / HTML 解析 / 前置缺失），此时没有资格判断是否漂移

    [WARN] / [INFO] 行（hash 不同、HTML 内联 version 为空串）不会改变退出码，
    但会出现在输出里，便于发布自检时用肉眼捕获。

    发布后自检用法详见 project-docs/05-deploy-docker.md「发布后自检」。
#>
[CmdletBinding()]
param(
    [string]$BaseUrl = 'https://sub2.lansn.icu',
    [string]$RepoRoot = '',
    [string]$LocalDistRoot = '',
    [int]$TimeoutSec = 30,
    # 严格模式：把"线上入口 hash == 本地 dist 入口 hash"也当成失败条件。
    # 默认关闭，因为 vite 产物 hash 受构建环境影响（node/pnpm 版本、模块
    # 排序、时间戳注入等），同一提交在同机重建也可能得到不同 hash —— 实测
    # 就出现过（线上 index-DEkhF0Ff.js vs 本地 index-BbceXYHS.js，同一 HEAD）。
    # 因此 hash 不一致只能证明"不是同一次构建"，不能证明"线上构建自不同提交"。
    [switch]$StrictDistHash
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# -----------------------------------------------------------------------------
# 0. 本地前置：仓库根与 dist 目录
# -----------------------------------------------------------------------------
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = Split-Path -Parent $PSScriptRoot
}
if (-not (Test-Path -LiteralPath $RepoRoot -PathType Container)) {
    Write-Host "FAIL 仓库根不存在: $RepoRoot" -ForegroundColor Red
    exit 2
}
$RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path

if ([string]::IsNullOrWhiteSpace($LocalDistRoot)) {
    $LocalDistRoot = Join-Path $RepoRoot 'backend/internal/web/dist'
}
$LocalIndexPath = Join-Path $LocalDistRoot 'index.html'

function Get-EntryAsset {
    param([string]$Html)
    # Vite 的入口脚本形如 <script type="module" crossorigin src="/assets/index-DEkhF0Ff.js">
    $m = [regex]::Match($Html, '/assets/index-([A-Za-z0-9_-]+)\.js')
    if (-not $m.Success) { return '' }
    return 'index-' + $m.Groups[1].Value + '.js'
}

function Get-InjectedVersion {
    param([string]$Html)
    # 后端把公开设置内联成 window.__APP_CONFIG__={...,"version":"..."}
    $m = [regex]::Match($Html, '"version"\s*:\s*"([^"]*)"')
    if (-not $m.Success) { return $null }
    return $m.Groups[1].Value
}

function Get-CommitFromVersion {
    param([string]$Version)
    # 约定形如 0.2.17-fork.f9d8bc855：最后一段点分后缀是 git 短提交。
    $m = [regex]::Match($Version, '\.([0-9a-f]{7,40})$')
    if (-not $m.Success) { return '' }
    return $m.Groups[1].Value
}

function Test-CommitMatch {
    param([string]$Expected, [string]$Actual)
    if ([string]::IsNullOrWhiteSpace($Expected) -or [string]::IsNullOrWhiteSpace($Actual)) {
        return $false
    }
    $e = $Expected.Trim().ToLowerInvariant()
    $a = $Actual.Trim().ToLowerInvariant()
    # 短提交按前缀比较：线上常是 9 位，本地 rev-parse --short 默认 7/8 位。
    if ($a.StartsWith($e) -or $e.StartsWith($a)) {
        return ($e.Length -ge 7 -and $a.Length -ge 7)
    }
    return $false
}

# -----------------------------------------------------------------------------
# 1. 线上取数
# -----------------------------------------------------------------------------
$BaseUrl = $BaseUrl.TrimEnd('/')
$cacheBuster = '_verify=' + [guid]::NewGuid().ToString('N')

try {
    $settings = Invoke-RestMethod -Uri ($BaseUrl + '/api/v1/settings/public?' + $cacheBuster) -TimeoutSec $TimeoutSec -Headers @{ Accept = 'application/json' }
} catch {
    Write-Host "FAIL 取 /api/v1/settings/public 失败: $($_.Exception.Message)" -ForegroundColor Red
    exit 2
}
if ($null -eq $settings -or $null -eq $settings.data) {
    Write-Host 'FAIL /api/v1/settings/public 返回体不含 data 字段' -ForegroundColor Red
    exit 2
}
$onlineVersion = ''
if ($settings.data.PSObject.Properties.Name -contains 'version') {
    $onlineVersion = [string]$settings.data.version
}

try {
    $indexResp = Invoke-WebRequest -Uri ($BaseUrl + '/?' + $cacheBuster) -TimeoutSec $TimeoutSec -SkipHttpErrorCheck
} catch {
    Write-Host "FAIL 取站点首页失败: $($_.Exception.Message)" -ForegroundColor Red
    exit 2
}
if ($indexResp.StatusCode -ne 200) {
    Write-Host "FAIL 站点首页 HTTP $($indexResp.StatusCode)" -ForegroundColor Red
    exit 2
}
$onlineHtml = [string]$indexResp.Content
$onlineAsset = Get-EntryAsset -Html $onlineHtml
if ([string]::IsNullOrWhiteSpace($onlineAsset)) {
    Write-Host 'FAIL 首页 HTML 里找不到 /assets/index-*.js 入口脚本（不是内嵌前端的构建，或被反代改写）' -ForegroundColor Red
    exit 2
}
$onlineInjectedVersion = Get-InjectedVersion -Html $onlineHtml

# -----------------------------------------------------------------------------
# 2. 本地取数
# -----------------------------------------------------------------------------
$headFull = ''
$headShort = ''
$headTime = ''
try {
    $headFull = (& git -C $RepoRoot rev-parse HEAD 2>$null | Select-Object -First 1)
    if ($null -ne $headFull) { $headFull = $headFull.Trim() }
    $headShort = (& git -C $RepoRoot rev-parse --short HEAD 2>$null | Select-Object -First 1)
    if ($null -ne $headShort) { $headShort = $headShort.Trim() }
    $headTime = (& git -C $RepoRoot show -s --format=%cI HEAD 2>$null | Select-Object -First 1)
    if ($null -ne $headTime) { $headTime = $headTime.Trim() }
} catch {
    Write-Host "FAIL 读取 git HEAD 失败: $($_.Exception.Message)" -ForegroundColor Red
    exit 2
}
if ([string]::IsNullOrWhiteSpace($headFull)) {
    Write-Host "FAIL $RepoRoot 不是 git 工作树，取不到 HEAD" -ForegroundColor Red
    exit 2
}

$localAsset = ''
$localDistTime = $null
$localIndexExists = Test-Path -LiteralPath $LocalIndexPath -PathType Leaf
if ($localIndexExists) {
    $localAsset = Get-EntryAsset -Html ([System.IO.File]::ReadAllText($LocalIndexPath))
    $localDistTime = (Get-Item -LiteralPath $LocalIndexPath).LastWriteTime
} else {
    Write-Host "WARN 本地 dist 缺失: $LocalIndexPath（先 cd frontend; corepack pnpm@9 run build）" -ForegroundColor Yellow
}

# 前端源码最后一次变更时间：dist 比它旧就说明本地前端产物是陈旧的。
$frontendSrcTime = $null
try {
    $raw = (& git -C $RepoRoot log -1 --format=%cI -- frontend/src frontend/index.html frontend/vite.config.ts frontend/public 2>$null | Select-Object -First 1)
    if (-not [string]::IsNullOrWhiteSpace($raw)) {
        $frontendSrcTime = [datetime]::Parse($raw.Trim(), [System.Globalization.CultureInfo]::InvariantCulture, [System.Globalization.DateTimeStyles]::AdjustToUniversal)
    }
} catch {
    $frontendSrcTime = $null
}

# -----------------------------------------------------------------------------
# 3. 判定
# -----------------------------------------------------------------------------
# 强判据：线上 version 解析出的 commit == 本地 HEAD。
# 这是唯一决定退出码的判据。
$onlineCommit = Get-CommitFromVersion -Version $onlineVersion
$strongOk = Test-CommitMatch -Expected $onlineCommit -Actual $headFull

# 弱判据：入口 hash 相同。<只作参考>，见 -StrictDistHash 的说明。
$distHashSame = ($localAsset -ne '') -and ($onlineAsset -eq $localAsset)

$localDistStale = $false
$staleNote = ''
if ($null -ne $localDistTime -and $null -ne $frontendSrcTime) {
    $distUtc = $localDistTime.ToUniversalTime()
    if ($distUtc -lt $frontendSrcTime) {
        $localDistStale = $true
        $staleNote = 'dist 早于前端源码最后提交'
    }
} elseif (-not $localIndexExists) {
    $localDistStale = $true
    $staleNote = 'dist 不存在'
}

# -----------------------------------------------------------------------------
# 4. 证据
# -----------------------------------------------------------------------------
Write-Host ''
Write-Host '================ 部署一致性校验 ================' -ForegroundColor Cyan
Write-Host ("站点          : " + $BaseUrl)
Write-Host ("仓库          : " + $RepoRoot)
Write-Host ''
Write-Host '--- 线上后端 ---'
Write-Host ("  /api/v1/settings/public version : " + $(if ($onlineVersion -eq '') { '<空>' } else { $onlineVersion }))
Write-Host ("  从中解析出的提交                : " + $(if ($onlineCommit -eq '') { '<无法解析>' } else { $onlineCommit }))
Write-Host '--- 线上前端 ---'
Write-Host ("  首页入口脚本                    : " + $onlineAsset)
Write-Host ("  HTML 内联 __APP_CONFIG__.version: " + $(if ($null -eq $onlineInjectedVersion) { '<未注入>' } elseif ($onlineInjectedVersion -eq '') { '<空串>' } else { $onlineInjectedVersion }))
Write-Host '--- 本地 ---'
Write-Host ("  git HEAD                        : " + $headFull.Substring(0, [Math]::Min(12, $headFull.Length)) + " (" + $headTime + ")")
Write-Host ("  dist/index.html                 : " + $(if ($localIndexExists) { $LocalIndexPath } else { '<缺失>' }))
Write-Host ("  dist 入口脚本                   : " + $(if ($localAsset -eq '') { '<无>' } else { $localAsset }))
if ($null -ne $localDistTime) {
    $distTimeLine = "  dist 生成时间                   : " + $localDistTime.ToString('yyyy-MM-dd HH:mm:ss')
    if ($localDistStale) { $distTimeLine += '（' + $staleNote + '）' }
    Write-Host $distTimeLine
}
if ($null -ne $frontendSrcTime) {
    Write-Host ("  前端源码最后提交                : " + $frontendSrcTime.ToLocalTime().ToString('yyyy-MM-dd HH:mm:ss'))
}
Write-Host ''

# -----------------------------------------------------------------------------
# 5. 三行结论
# -----------------------------------------------------------------------------
$tagOk = '[OK]  '
$tagBad = '[DRIFT]'

if ($onlineVersion -eq '') {
    $backendLine = $tagBad + ' 线上后端 : version 为空串，后端没有注入构建版本（检查 ldflags -X main.Version）'
} elseif ($onlineCommit -eq '') {
    $backendLine = $tagBad + ' 线上后端 : ' + $onlineVersion + ' 无法解析出提交，约定应为 <版本>-fork.<短提交>'
} elseif ($strongOk) {
    $backendLine = $tagOk + ' 线上后端 : ' + $onlineVersion + ' == 本地 HEAD ' + $headShort + '（强判据）'
} else {
    $backendLine = $tagBad + ' 线上后端 : ' + $onlineVersion + ' (提交 ' + $onlineCommit + ') != 本地 HEAD ' + $headShort + '（强判据）'
}

if (-not $localIndexExists) {
    $frontendLine = '[INFO] 线上前端 : ' + $onlineAsset + ' | 本地 dist 缺失（先 cd frontend; corepack pnpm@9 run build），弱判据跳过'
} elseif ($distHashSame) {
    $frontendLine = $tagOk + ' 线上前端 : ' + $onlineAsset + ' == 本地 dist（同一次构建）'
} elseif ($StrictDistHash) {
    $frontendLine = $tagBad + ' 线上前端 : ' + $onlineAsset + ' != 本地 dist ' + $localAsset + '（-StrictDistHash 已开启，判为不一致）'
} else {
    $frontendLine = '[INFO] 线上前端 : ' + $onlineAsset + ' != 本地 dist ' + $localAsset + '（弱判据：hash 不同只说明不是同一次构建，不能证明线上构建自不同提交）'
}

$headLine = $tagOk + ' 本地 HEAD : ' + $headFull
if ($localDistStale) {
    $headLine = $tagBad + ' 本地 HEAD : ' + $headFull + ' | ' + $staleNote + '，直接 go build -tags embed 会内嵌旧前端（先 cd frontend; corepack pnpm@9 run build）'
}

# 第四行单独给「HTML 内联 version」下结论，不再只是附注：
# window.__APP_CONFIG__.version 为空说明后端没有把构建版本注入 HTML，
# 前端首屏拿不到版本（缺陷 D-02）。修复已落在 setting_public.go 的
# serviceBuildVersion + Dockerfile 的第二处 -X，但【要下一次构建才生效】，
# 所以对线上当前这个空串只报 WARN（不改变退出码），
# 让它出现在发布自检输出里被一眼看到，而不是静默通过。
if ($null -eq $onlineInjectedVersion) {
    $injectLine = '[WARN] 内联版本 : HTML 里没有 window.__APP_CONFIG__（不是内嵌前端的构建，或被反代改写了 HTML）'
    $injectColor = 'Yellow'
    $injectOk = $false
} elseif ($onlineInjectedVersion -eq '') {
    $injectLine = '[WARN] 内联版本 : HTML 内联 version 为空串，前端首屏拿不到版本（缺陷 D-02；修复在 setting_public.go + Dockerfile，需下次构建生效）'
    $injectColor = 'Yellow'
    $injectOk = $false
} elseif ($onlineInjectedVersion -eq $onlineVersion) {
    $injectLine = $tagOk + ' 内联版本 : ' + $onlineInjectedVersion + ' == /api/v1/settings/public version'
    $injectColor = 'Green'
    $injectOk = $true
} else {
    $injectLine = '[WARN] 内联版本 : ' + $onlineInjectedVersion + ' != /api/v1/settings/public version ' + $onlineVersion + '（两处版本注入不同步，检查 -X 是否只注入了一处）'
    $injectColor = 'Yellow'
    $injectOk = $false
}

$frontendColor = if ($distHashSame) { 'Green' } elseif ($StrictDistHash) { 'Red' } else { 'DarkGray' }

Write-Host $backendLine -ForegroundColor $(if ($strongOk) { 'Green' } else { 'Red' })
Write-Host $frontendLine -ForegroundColor $frontendColor
Write-Host $injectLine -ForegroundColor $injectColor
Write-Host $headLine -ForegroundColor $(if ($localDistStale) { 'Yellow' } else { 'Green' })
Write-Host ''

# 退出码语义（对外契约，写完文档也不要改）：
#   0 = 强判据通过（弱判据不一致默认只是 [INFO]，-StrictDistHash 下升为失败）
#   1 = 强判据失败（[DRIFT] 行），或 -StrictDistHash 下弱判据失败
#   2 = 取数失败（网络 / HTML 解析 / 前置缺失），没资格判断是否漂移
# [WARN]/[INFO] 行（hash 不同、内联 version 空串）不影响退出码。
$weakFailed = $StrictDistHash -and (-not $distHashSame)
$warned = (-not $injectOk) -or ((-not $distHashSame) -and (-not $StrictDistHash))

if ($strongOk -and (-not $weakFailed)) {
    if ($warned) {
        Write-Host 'RESULT: PASS（强判据通过；有 [WARN]/[INFO] 行，请肉眼确认）' -ForegroundColor Green
    } else {
        Write-Host 'RESULT: PASS' -ForegroundColor Green
    }
    exit 0
}
Write-Host 'RESULT: FAIL（见上方 [DRIFT] 行）' -ForegroundColor Red
exit 1
