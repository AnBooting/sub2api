#Requires -Version 7.0
<#
.SYNOPSIS
    本地合并前门禁：一键执行 project-docs/04-git-workflow.md §9 的检查清单。

.DESCRIPTION
    本仓库 GitHub Actions 实测停摆（最近一次 run 是 2026-10-22，之后 push/PR 没有 run，
    PR 的 statusCheckRollup 为空），合并判断只能靠本机。本脚本就是那份清单的实现，
    可作为 pre-push 钩子或合并前手动检查。

    Scope:
      docs     空白/行尾（git diff --check）+ 敏感文件名扫描
      backend  go 版本一致性、gofmt、go build（full 模式加 unit 测试）
      frontend vue-tsc -b（full 模式加 check:i18n / vitest / vite build）
      all      docs + backend + frontend（默认）

.PARAMETER Scope
    docs | backend | frontend | all，默认 all。

.PARAMETER Mode
    quick（默认）：编译/类型检查，适合 pre-push。
    full：额外跑单测、i18n 与前端构建，适合合并前。

.PARAMETER RepoRoot
    仓库根目录，默认按脚本位置推导（tools/ 的上一级）。

.EXAMPLE
    pwsh -File tools/pre-push-check.ps1 -Scope docs
    pwsh -File tools/pre-push-check.ps1 -Scope backend -Mode full
    pwsh -File tools/pre-push-check.ps1 -Mode full

.NOTES
    退出码：0 = 全部通过；1 = 至少一项失败。
    已知环境失败（不算本 diff 引入）：
      - internal/repository 的 3 条 TestPgDumper* 需要 sh，本机没有
      - TestInflightEstimate_AccountMappingNoDBAndBoundedMemory 满载时偶发
#>
param(
    [ValidateSet('docs', 'backend', 'frontend', 'all')]
    [string]$Scope = 'all',
    [ValidateSet('quick', 'full')]
    [string]$Mode = 'quick',
    [string]$RepoRoot
)

$ErrorActionPreference = 'Continue'
if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent $PSScriptRoot }
$backendDir = Join-Path $RepoRoot 'backend'
$frontendDir = Join-Path $RepoRoot 'frontend'
$script:results = @()

function Add-Result {
    param([string]$Name, [int]$Code, [string]$Detail = '')
    $ok = ($Code -eq 0)
    $script:results += [pscustomobject]@{ Name = $Name; Ok = $ok; Code = $Code; Detail = $Detail }
    $tag = if ($ok) { 'PASS' } else { 'FAIL' }
    $color = if ($ok) { 'Green' } else { 'Red' }
    Write-Host ("[{0}] {1} (exit {2})" -f $tag, $Name, $Code) -ForegroundColor $color
    if (-not $ok -and $Detail) { Write-Host $Detail }
}

function Invoke-Step {
    param([string]$Name, [scriptblock]$Block)
    $out = (& $Block) 2>&1 | Out-String
    $code = if ($null -eq $LASTEXITCODE) { 1 } else { [int]$LASTEXITCODE }
    Add-Result $Name $code $out.Trim()
    return $code
}

if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot '.git'))) {
    Write-Host "RepoRoot 不是 git 仓库：$RepoRoot" -ForegroundColor Red
    exit 1
}

Write-Host "== 仓库：$RepoRoot"
$headOut = (& git -C $RepoRoot log -1 --oneline) 2>&1 | Out-String
Write-Host ("== HEAD：{0}" -f $headOut.Trim())

# ---------- docs ----------
if ($Scope -in @('docs', 'all')) {
    [void](Invoke-Step 'whitespace: git diff --check' { git -C $RepoRoot diff --check })
    [void](Invoke-Step 'whitespace: git diff --cached --check' { git -C $RepoRoot diff --cached --check })

    $base = 'origin/develop'
    $hasBase = (& git -C $RepoRoot rev-parse --verify --quiet $base) 2>$null
    if ($hasBase) {
        $changed = @((& git -C $RepoRoot diff --name-only "$base...HEAD") 2>$null)
        $changed += @((& git -C $RepoRoot diff --name-only --cached) 2>$null)
        $changed = $changed | Where-Object { $_ } | Sort-Object -Unique
        $suspect = $changed | Where-Object {
            $_ -match '(.env$|^data/|postgres_data/|redis_data/|secret|credential|password|.pem$|.key$|id_rsa)'
        }
        if ($suspect) {
            Add-Result 'secrets: 敏感文件名扫描' 1 (($suspect | Out-String).Trim())
        } else {
            Add-Result 'secrets: 敏感文件名扫描' 0 "基线 $base，改动 $($changed.Count) 个文件"
        }
    } else {
        Add-Result 'secrets: 敏感文件名扫描' 0 "跳过：本地没有 $base"
    }
}

# ---------- backend ----------
if ($Scope -in @('backend', 'all')) {
    $env:GOROOT = [Environment]::GetEnvironmentVariable('GOROOT', 'User')
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'User') + ';' + [Environment]::GetEnvironmentVariable('Path', 'Machine')
    if (-not $env:GOPATH) { $env:GOPATH = 'F:\tools\gopath' }
    $env:GOTOOLCHAIN = 'local'

    [void](Invoke-Step 'backend: go version' { go version })

    $modGo = ''
    $modPath = Join-Path $backendDir 'go.mod'
    if (Test-Path -LiteralPath $modPath) {
        $m = Select-String -LiteralPath $modPath -Pattern '^go\s+(?<v>[0-9]+\.[0-9]+(\.[0-9]+)?)' | Select-Object -First 1
        if ($m) { $modGo = $m.Matches[0].Groups['v'].Value }
    }
    $goOut = (& go version) 2>&1 | Out-String
    if ($modGo -and ($goOut -match 'go(?<tc>[0-9]+\.[0-9.]+)')) {
        $toolchain = $matches['tc']
        $want = $modGo
        $ok = ($want -eq $toolchain) -or ($toolchain.StartsWith($want + '.')) -or ($want.StartsWith($toolchain + '.'))
        if ($ok) {
            Add-Result 'backend: go.mod 与工具链一致' 0 "go.mod=$want / go$toolchain"
        } else {
            Add-Result 'backend: go.mod 与工具链一致' 1 "go.mod=$want / go$toolchain"
        }
    } else {
        Add-Result 'backend: go.mod 与工具链一致' 0 '未解析到版本，跳过比较'
    }

    $goTargets = @()
    foreach ($args in @(@('diff', '--name-only', 'origin/develop...HEAD', '--', '*.go'),
                        @('diff', '--name-only', '--cached', '--', '*.go'),
                        @('diff', '--name-only', '--', '*.go'))) {
        $goTargets += @((& git -C $RepoRoot @args) 2>$null)
    }
    $goTargets = $goTargets | Where-Object { $_ } | Sort-Object -Unique | Where-Object { Test-Path -LiteralPath (Join-Path $RepoRoot $_) }
    if (@($goTargets).Count -gt 0) {
        $fmtList = @($goTargets)
        $fmtOut = (& gofmt -l $fmtList) 2>&1 | Out-String
        $fmtCode = if ($fmtOut.Trim()) { 1 } else { 0 }
        Add-Result 'backend: gofmt' $fmtCode $fmtOut.Trim()
    } else {
        Add-Result 'backend: gofmt' 0 '没有改动的 .go 文件，跳过'
    }

    Push-Location $backendDir
    try {
        [void](Invoke-Step 'backend: go build' { go build -o bin/server.exe ./cmd/server })
        if ($Mode -eq 'full') {
            [void](Invoke-Step 'backend: go test -tags=unit ./...' { go test -p 1 -tags=unit ./... })
        } else {
            Add-Result 'backend: 单测（quick 跳过）' 0 '需要单测请用 -Mode full'
        }
    } finally {
        Pop-Location
    }
}

# ---------- frontend ----------
if ($Scope -in @('frontend', 'all')) {
    if (-not (Test-Path -LiteralPath (Join-Path $frontendDir 'node_modules'))) {
        Add-Result 'frontend: 依赖' 1 '缺少 frontend/node_modules，先跑 corepack pnpm@9 install --frozen-lockfile'
    } else {
        Push-Location $frontendDir
        try {
            [void](Invoke-Step 'frontend: vue-tsc -b' { corepack pnpm@9 exec vue-tsc -b })
            if ($Mode -eq 'full') {
                [void](Invoke-Step 'frontend: check:i18n' { corepack pnpm@9 run check:i18n })
                [void](Invoke-Step 'frontend: vitest run' { node node_modules/vitest/vitest.mjs run --reporter=dot })
                [void](Invoke-Step 'frontend: vite build' { corepack pnpm@9 exec vite build })
            } else {
                Add-Result 'frontend: 单测/构建（quick 跳过）' 0 '需要它们请用 -Mode full'
            }
        } finally {
            Pop-Location
        }
    }
}

# ---------- 汇总 ----------
Write-Host ''
Write-Host '== 汇总'
foreach ($item in $results) {
    $tag = if ($item.Ok) { 'PASS' } else { 'FAIL' }
    Write-Host ("[{0}] {1}" -f $tag, $item.Name)
}
$failed = @($results | Where-Object { -not $_.Ok })
if ($failed.Count -eq 0) {
    Write-Host ("全部通过（{0} 项）" -f $results.Count) -ForegroundColor Green
    exit 0
}
Write-Host ("失败 {0} 项 / 共 {1} 项" -f $failed.Count, $results.Count) -ForegroundColor Red
exit 1
