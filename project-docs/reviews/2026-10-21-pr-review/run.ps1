param(
    [Parameter(Mandatory)][string]$PR5Root,
    [Parameter(Mandatory)][string]$PR7Root,
    [Parameter(Mandatory)][string]$PR8Root,
    [Parameter(Mandatory)][string]$PR9Root,
    [string]$GoExe = 'go',
    [string]$NodeExe = 'node'
)
$ErrorActionPreference = 'Stop'
$taskRoots = @{5=(Resolve-Path -LiteralPath $PR5Root).Path;7=(Resolve-Path -LiteralPath $PR7Root).Path;8=(Resolve-Path -LiteralPath $PR8Root).Path;9=(Resolve-Path -LiteralPath $PR9Root).Path}
$taskFiles = @(
    @(5,'review_live_followup_test.go','backend/internal/service'),
    @(5,'review_live_settlement_test.go','backend/internal/service'),
    @(5,'review_live_recovery_test.go','backend/internal/service'),
    @(5,'review_panic_status_test.go','backend/internal/server/middleware'),
    @(7,'review-route-transition.spec.ts','frontend/src'),
    @(8,'review_video_task_export_test.go','backend/internal/service'),
    @(8,'review_video_task_response_test.go','backend/internal/service'),
    @(8,'review_video_task_privacy_test.go','backend/internal/handler/dto'),
    @(9,'review_renamed_migration_test.go','backend/internal/repository')
)
$taskCreated = [Collections.Generic.List[string]]::new()
$taskFailed = $false
foreach ($taskFile in $taskFiles) {
    $taskTarget = Join-Path $taskRoots[$taskFile[0]] ($taskFile[2] + '/' + $taskFile[1])
    if (Test-Path -LiteralPath $taskTarget) { throw "Refusing to overwrite existing file: $taskTarget" }
}
try {
    foreach ($taskFile in $taskFiles) {
        $taskTarget = Join-Path $taskRoots[$taskFile[0]] ($taskFile[2] + '/' + $taskFile[1])
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot ('tests/' + $taskFile[1])) -Destination $taskTarget
        $taskCreated.Add($taskTarget)
    }
    foreach ($taskPR in @(5,8,9)) {
        Push-Location (Join-Path $taskRoots[$taskPR] 'backend')
        try {
            $taskPackages = @(switch ($taskPR) {
                5 { @('./internal/service','./internal/server/middleware') }
                8 { @('./internal/service','./internal/handler/dto') }
                9 { @('./internal/repository') }
            })
            & $GoExe test -p 4 -tags=unit @taskPackages -run '^TestReview' -count=1 -v
            Write-Output "PR #$taskPR review tests exit code: $LASTEXITCODE"
            if ($LASTEXITCODE -ne 0) { $taskFailed = $true }
        } finally { Pop-Location }
    }
    Push-Location (Join-Path $taskRoots[7] 'frontend')
    try {
        & $NodeExe node_modules/vitest/vitest.mjs run src/review-route-transition.spec.ts --maxWorkers=2 --minWorkers=1
        Write-Output "PR #7 route regression exit code: $LASTEXITCODE"
        if ($LASTEXITCODE -ne 0) { $taskFailed = $true }
    } finally { Pop-Location }
} finally {
    foreach ($taskTarget in $taskCreated) { Remove-Item -LiteralPath $taskTarget -Force }
}
if ($taskFailed) { exit 1 }
