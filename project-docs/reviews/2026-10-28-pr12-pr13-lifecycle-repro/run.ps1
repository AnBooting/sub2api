param([Parameter(Mandatory=$true)][string]$BackendPath)
$ErrorActionPreference = 'Stop'
$backend = (Resolve-Path -LiteralPath $BackendPath).Path
$target = Join-Path $backend 'internal\service\review_lifecycle_backoff_test.go'
if (Test-Path -LiteralPath $target) { throw "Refusing to replace existing file: $target" }
$oldLocation = Get-Location
try {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'review_lifecycle_backoff_test.go') -Destination $target
    Set-Location -LiteralPath $backend
    & go test -tags=unit -ldflags=-w ./internal/service -run '^TestReviewExistingLifecycleCaseMustNotUseProductionSettlementBackoff$' -count=1 -v
    $reproExit = $LASTEXITCODE
} finally {
    Set-Location -LiteralPath $oldLocation.Path
    if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Force }
}
exit $reproExit
