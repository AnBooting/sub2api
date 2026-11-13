<#
.SYNOPSIS
校验 project-docs/00-overview.md §7 的 PR 状态表是否与远端事实一致。

状态表靠人工维护会过期。本脚本只做比对，不改文档；由 pre-push-check 的 docs 作用域调用。
远端不可用时 SKIP，不阻断本地提交。

.EXAMPLE
pwsh -File tools/check-project-status.ps1
#>
param(
    [string]$RepoRoot,
    [string]$Repo = 'AnBooting/sub2api'
)

$ErrorActionPreference = 'Continue'
if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent $PSScriptRoot }
$overview = Join-Path $RepoRoot 'project-docs/00-overview.md'
if (-not (Test-Path -LiteralPath $overview)) { Write-Host 'SKIP: 没有 project-docs/00-overview.md'; exit 0 }

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { Write-Host 'SKIP: 本机没有 gh'; exit 0 }

$raw = (& gh pr list --repo $Repo --state all --limit 200 --json number,state,headRefName,mergeCommit) 2>&1 | Out-String
if ($LASTEXITCODE -ne 0) { Write-Host 'SKIP: gh 无法访问远端（网络或鉴权），不阻断'; exit 0 }

try { $prs = @($raw | ConvertFrom-Json) } catch { Write-Host 'SKIP: gh 输出无法解析'; exit 0 }
$text = Get-Content -LiteralPath $overview -Raw
$problems = @()
$open = @($prs | Where-Object { $_.state -eq 'OPEN' })

foreach ($pr in $open) {
    $row = [regex]::Match($text, '\| #' + $pr.number + ' \|[^\r\n]*')
    if (-not $row.Success) {
        $problems += "OPEN PR #$($pr.number)（$($pr.headRefName)）不在 §7 表中"
    } elseif ($row.Value -match 'MERGED' -and $row.Value -notmatch 'REVERT') {
        $problems += "PR #$($pr.number) 远端正 OPEN，表里写成 MERGED"
    }
}

$stated = [regex]::Match($text, '当前\s*(\d+)\s*个\s*OPEN PR')
if ($stated.Success -and [int]$stated.Groups[1].Value -ne $open.Count) {
    $problems += "正文声明 $($stated.Groups[1].Value) 个 OPEN PR，远端实际 $($open.Count) 个"
}

if ($problems.Count -gt 0) {
    Write-Host "PR 状态表与远端事实不一致（共 $($prs.Count) 个 PR，OPEN $($open.Count) 个）："
    $problems | ForEach-Object { Write-Host "  - $_" }
    Write-Host '更新 project-docs/00-overview.md §7 后重跑。'
    exit 1
}

Write-Host "PR 状态表与远端一致（OPEN $($open.Count) / 共 $($prs.Count)）。"
exit 0
