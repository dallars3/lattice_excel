# verify-test-db-isolation.ps1: repeatable acceptance runner for the test-database
# isolation guard (2026-11-18 shared-dev-database incident).
#
# Protocol: with the live stack RUNNING (do not stop it), snapshot the shared dev
# databases excel_core/excel_worker (table set + per-table exact row counts), run
# the full core and worker go test suites (-p 1, repo convention), then compare the
# snapshot again. All tests green + snapshot unchanged = backend tests only touched
# the *_test databases and never the shared databases.
#
# Usage (repo root):
#   powershell -ExecutionPolicy Bypass -File scripts/verify-test-db-isolation.ps1
#
# Prerequisites: local MySQL (127.0.0.1:3306) and test databases
# excel_core_test / excel_worker_test. Missing databases get a one-shot
# CREATE DATABASE hint from the guard; rules live in the excel-libs/testmysql
# package doc. Snapshot test itself: excel_core_service/internal/testisolation.
#
# NOTE: keep this file ASCII-only (repo ps1 convention, no BOM).

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$coreDir = Join-Path $repoRoot "excel_core_service"
$workerDir = Join-Path $repoRoot "excel_worker_service"

# Go proxy convention from the repo agent notes + proto generation first.
$env:GOPROXY = "https://goproxy.cn,direct"
Push-Location $repoRoot
try {
    powershell -ExecutionPolicy Bypass -File docs/tools/generate-proto.ps1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "generate-proto.ps1 failed" }
} finally {
    Pop-Location
}

$snapshotFile = Join-Path $env:TEMP ("ad01-shared-db-snapshot-{0}.json" -f [guid]::NewGuid().ToString("N"))

function Invoke-SnapshotTest {
    param([string]$Mode)
    $env:AD01_SNAPSHOT_FILE = $snapshotFile
    $env:AD01_SNAPSHOT_MODE = $Mode
    try {
        Push-Location $coreDir
        try {
            go test ./internal/testisolation/ -run TestSharedDevDatabaseSnapshot -count=1 -v
            if ($LASTEXITCODE -ne 0) { throw "snapshot $Mode failed" }
        } finally {
            Pop-Location
        }
    } finally {
        Remove-Item Env:AD01_SNAPSHOT_FILE -ErrorAction SilentlyContinue
        Remove-Item Env:AD01_SNAPSHOT_MODE -ErrorAction SilentlyContinue
    }
}

function Invoke-FullTests {
    param([string]$Dir, [string]$Label)
    Push-Location $Dir
    try {
        Write-Host "=== $Label full go test -p 1 -count=1 (live stack stays running) ===" -ForegroundColor Cyan
        go test -p 1 -count=1 ./...
        if ($LASTEXITCODE -ne 0) { throw "$Label go test failed" }
    } finally {
        Pop-Location
    }
}

Write-Host "=== record shared dev database snapshot ===" -ForegroundColor Cyan
Invoke-SnapshotTest -Mode "record"

Invoke-FullTests -Dir $coreDir -Label "excel_core_service"
Invoke-FullTests -Dir $workerDir -Label "excel_worker_service"

Write-Host "=== verify shared dev database snapshot unchanged ===" -ForegroundColor Cyan
Invoke-SnapshotTest -Mode "verify"

Remove-Item $snapshotFile -ErrorAction SilentlyContinue
Write-Host "PASS: full suites green; shared dev databases (excel_core/excel_worker) unchanged before/after." -ForegroundColor Green
