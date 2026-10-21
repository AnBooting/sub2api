param([Parameter(Mandatory = $true)][string]$RepoRoot)
$ErrorActionPreference = 'Stop'
$taskRepo = (Resolve-Path -LiteralPath $RepoRoot).Path
$taskFrontend = Join-Path $taskRepo 'frontend'
$taskNode = (Get-Command node -ErrorAction Stop).Source
$taskVitest = Join-Path $taskFrontend 'node_modules/vitest/vitest.mjs'
if (-not (Test-Path -LiteralPath $taskVitest)) { throw 'Frontend dependencies are required.' }
$taskName = 'review-pr7-' + [guid]::NewGuid().ToString('N') + '.spec.ts'
$taskTest = Join-Path $taskFrontend ('src/' + $taskName)
$taskExit = 1
try {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'tests/review-pr7.spec.ts') -Destination $taskTest
    Push-Location -LiteralPath $taskFrontend
    try {
        & $taskNode $taskVitest run ('src/' + $taskName) --maxWorkers=1 --minWorkers=1
        $taskExit = $LASTEXITCODE
    } finally { Pop-Location }
} finally {
    if (Test-Path -LiteralPath $taskTest) { Remove-Item -LiteralPath $taskTest -Force }
}
exit $taskExit
