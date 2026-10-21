param([string]$PR5RepoRoot, [string]$PR7RepoRoot)
$ErrorActionPreference = 'Stop'
if (-not $PR5RepoRoot -and -not $PR7RepoRoot) { throw 'Specify -PR5RepoRoot and/or -PR7RepoRoot.' }
$taskResult = 0

if ($PR5RepoRoot) {
    $taskRoot = (Resolve-Path -LiteralPath $PR5RepoRoot).Path
    $taskCopies = @(
        @{ Source = 'review_live_settlement_test.go'; Target = 'backend/internal/service/review_live_settlement_test.go' },
        @{ Source = 'review_panic_status_test.go'; Target = 'backend/internal/server/middleware/review_panic_status_test.go' }
    )
    $taskCreated = @()
    try {
        foreach ($taskCopy in $taskCopies) {
            $taskTarget = Join-Path $taskRoot $taskCopy.Target
            if (Test-Path -LiteralPath $taskTarget) { throw "Test already exists: $taskTarget" }
            Copy-Item -LiteralPath (Join-Path $PSScriptRoot ('tests/' + $taskCopy.Source)) -Destination $taskTarget
            $taskCreated += $taskTarget
        }
        Push-Location -LiteralPath (Join-Path $taskRoot 'backend')
        try {
            go test -p 4 -tags=unit ./internal/service ./internal/server/middleware -run '^TestReview' -count=1
            if ($LASTEXITCODE -ne 0) { $taskResult = 1 }
        } finally { Pop-Location }
    } finally {
        foreach ($taskFile in $taskCreated) { Remove-Item -LiteralPath $taskFile -Force }
    }
}

if ($PR7RepoRoot) {
    $taskRoot = (Resolve-Path -LiteralPath $PR7RepoRoot).Path
    $taskFrontend = Join-Path $taskRoot 'frontend'
    $taskTarget = Join-Path $taskFrontend 'src/review-route-transition.spec.ts'
    if (Test-Path -LiteralPath $taskTarget) { throw "Test already exists: $taskTarget" }
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'tests/review-route-transition.spec.ts') -Destination $taskTarget
    try {
        Push-Location -LiteralPath $taskFrontend
        try {
            node node_modules/vitest/vitest.mjs run src/review-route-transition.spec.ts --maxWorkers=1 --minWorkers=1
            if ($LASTEXITCODE -ne 0) { $taskResult = 1 }
        } finally { Pop-Location }
    } finally { Remove-Item -LiteralPath $taskTarget -Force }
}
exit $taskResult
