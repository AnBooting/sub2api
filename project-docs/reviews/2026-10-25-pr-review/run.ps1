param(
    [Parameter(Mandatory)][string]$PR11Root,
    [Parameter(Mandatory)][string]$PR12Root,
    [Parameter(Mandatory)][string]$PR13Root,
    [string]$GoExe = 'go'
)
$ErrorActionPreference = 'Stop'
$taskRoots = @{11=(Resolve-Path -LiteralPath $PR11Root).Path;12=(Resolve-Path -LiteralPath $PR12Root).Path;13=(Resolve-Path -LiteralPath $PR13Root).Path}
$taskFiles = @(
    @(11,'review_migration_history_test.go','backend/internal/repository'),
    @(11,'review_renamed_migration_test.go','backend/internal/repository'),
    @(12,'review_live_combined_failure_test.go','backend/internal/service'),
    @(12,'review_live_followup_test.go','backend/internal/service'),
    @(12,'review_live_recovery_test.go','backend/internal/service'),
    @(12,'review_live_settlement_test.go','backend/internal/service'),
    @(13,'review_video_task_export_test.go','backend/internal/service'),
    @(13,'review_video_task_response_test.go','backend/internal/service'),
    @(13,'review_video_task_privacy_test.go','backend/internal/handler/dto')
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
    foreach ($taskPR in @(11,12,13)) {
        Push-Location (Join-Path $taskRoots[$taskPR] 'backend')
        try {
            $taskPackages = @(switch ($taskPR) {
                11 { './internal/repository' }
                12 { './internal/service' }
                13 { './internal/service'; './internal/handler/dto' }
            })
            & $GoExe test -p 2 -tags=unit @taskPackages -run '^TestReview|TestVideoTaskPublic|TestToVideoTask' -count=1 -timeout=4m -v
            Write-Output "PR #$taskPR review test exit code: $LASTEXITCODE"
            if ($LASTEXITCODE -ne 0) { $taskFailed = $true }
        } finally { Pop-Location }
    }
} finally {
    foreach ($taskTarget in $taskCreated) { Remove-Item -LiteralPath $taskTarget -Force }
}
if ($taskFailed) { exit 1 }
