param(
    [Parameter(Mandatory = $true)]
    [string]$RepoRoot
)

$ErrorActionPreference = 'Stop'
$taskRepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path
$taskFrontendRoot = Join-Path $taskRepoRoot 'frontend'
$taskNodePath = (Get-Command node -ErrorAction Stop).Source
$taskVitestPath = Join-Path $taskFrontendRoot 'node_modules/vitest/vitest.mjs'
if (-not (Test-Path -LiteralPath $taskVitestPath)) {
    throw 'Install the target repository frontend dependencies with pnpm 9 before running.'
}
if (-not (Test-Path -LiteralPath (Join-Path $taskRepoRoot 'tools/repo-map.mjs'))) {
    throw 'RepoRoot does not contain tools/repo-map.mjs.'
}

$taskTestName = 'pr5-review-icons-' + [guid]::NewGuid().ToString('N') + '.spec.ts'
$taskTemporaryTest = Join-Path $taskFrontendRoot ('src/__tests__/' + $taskTestName)
$taskOldRootVariable = [Environment]::GetEnvironmentVariable('SUB2API_REVIEW_ROOT', 'Process')
$taskUiExitCode = 1
$taskCliExitCode = 1

try {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'tests/keyusage-icons.spec.ts') -Destination $taskTemporaryTest
    Push-Location -LiteralPath $taskFrontendRoot
    try {
        & $taskNodePath $taskVitestPath run ('src/__tests__/' + $taskTestName) --maxWorkers=1 --minWorkers=1
        $taskUiExitCode = $LASTEXITCODE
    } finally {
        Pop-Location
    }

    $env:SUB2API_REVIEW_ROOT = $taskRepoRoot
    & $taskNodePath --test (Join-Path $PSScriptRoot 'tests/repo-map-impact.test.mjs')
    $taskCliExitCode = $LASTEXITCODE
} finally {
    if (Test-Path -LiteralPath $taskTemporaryTest) {
        Remove-Item -LiteralPath $taskTemporaryTest -Force
    }
    [Environment]::SetEnvironmentVariable('SUB2API_REVIEW_ROOT', $taskOldRootVariable, 'Process')
}

Write-Host "SVG tests exit code: $taskUiExitCode; CLI test exit code: $taskCliExitCode"
if ($taskUiExitCode -ne 0 -or $taskCliExitCode -ne 0) { exit 1 }
exit 0
