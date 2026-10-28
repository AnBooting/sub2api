param(
    [Parameter(Mandatory)][string]$SourceRoot,
    [string]$GoExe = 'go',
    [string]$PythonExe = 'python',
    [switch]$MigrationsOnly
)
$ErrorActionPreference = 'Stop'
$taskRoot = (Resolve-Path -LiteralPath $SourceRoot).Path
$taskFiles = @(
    @('review_legacy_integration_contract_test.go','backend/internal/repository'),
    @('review_migration_history_test.go','backend/internal/repository')
)
if (-not $MigrationsOnly) {
    $taskFiles += @(
        @('review_live_read_glitch_test.go','backend/internal/service'),
        @('review_live_audit_privacy_test.go','backend/internal/service')
    )
}
$taskGenerated = Join-Path $taskRoot 'backend/internal/repository/review_original_legacy_scenario_test.go'
$taskCreated = [Collections.Generic.List[string]]::new()
foreach ($taskFile in $taskFiles) {
    $taskTarget = Join-Path $taskRoot ($taskFile[1] + '/' + $taskFile[0])
    if (Test-Path -LiteralPath $taskTarget) { throw "Refusing to overwrite $taskTarget" }
}
if (Test-Path -LiteralPath $taskGenerated) { throw "Refusing to overwrite $taskGenerated" }
$taskExit = 0
try {
    foreach ($taskFile in $taskFiles) {
        $taskTarget = Join-Path $taskRoot ($taskFile[1] + '/' + $taskFile[0])
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot ('tests/' + $taskFile[0])) -Destination $taskTarget
        $taskCreated.Add($taskTarget)
    }
    $taskCreated.Add($taskGenerated)
    & $PythonExe (Join-Path $PSScriptRoot 'prepare_legacy_replay.py') $taskRoot
    if ($LASTEXITCODE -ne 0) { throw 'Cannot prepare original integration-scenario replay' }
    $taskPackages = @('./internal/repository')
    if (-not $MigrationsOnly) { $taskPackages += './internal/service' }
    Push-Location (Join-Path $taskRoot 'backend')
    try {
        & $GoExe test -p 2 -ldflags=-w -tags=unit @taskPackages -run '^TestRenamedMigrationReplay|^TestPostgresErrorCode|^TestReview' -count=1 -timeout=4m -v
        $taskExit = $LASTEXITCODE
    } finally { Pop-Location }
} finally {
    foreach ($taskTarget in $taskCreated) {
        if (Test-Path -LiteralPath $taskTarget) { Remove-Item -LiteralPath $taskTarget -Force }
    }
}
exit $taskExit
