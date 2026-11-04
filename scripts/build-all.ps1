# build-all.ps1：单入口串联「前端构建 → 拷贝产物到 embed 目录 → 后端编译」。
# 用法：powershell -ExecutionPolicy Bypass -File scripts\build-all.ps1 [-SkipFrontend]
param(
    [switch]$SkipFrontend
)

$ErrorActionPreference = 'Stop'
$env:GOPROXY = 'https://goproxy.cn,direct'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$webDir = Join-Path $Root 'web'
$distDir = Join-Path $webDir 'dist'
$embedDir = Join-Path $Root 'excel_web_api\internal\static\dist'
$apiDir = Join-Path $Root 'excel_web_api'

Push-Location $Root
try {
    if (-not $SkipFrontend -and (Test-Path -LiteralPath (Join-Path $webDir 'package.json'))) {
        Push-Location $webDir
        try {
            if (-not (Test-Path -LiteralPath (Join-Path $webDir 'node_modules'))) {
                & npm install
                if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败（npm install）' }
            }
            & npm run build
            if ($LASTEXITCODE -ne 0) { throw '前端构建失败（npm run build）' }
        }
        finally {
            Pop-Location
        }

        if (-not (Test-Path -LiteralPath $distDir)) {
            throw "前端产物目录不存在: $distDir"
        }

        # 清掉 embed 目录旧产物（保留提交的占位 index.html 供覆盖，避免 git 记删除）。
        if (Test-Path -LiteralPath $embedDir) {
            Get-ChildItem -LiteralPath $embedDir -Force |
                Where-Object { $_.Name -ne 'index.html' } |
                Remove-Item -Recurse -Force
        }
        else {
            New-Item -ItemType Directory -Path $embedDir -Force | Out-Null
        }
        Copy-Item -Path (Join-Path $distDir '*') -Destination $embedDir -Recurse -Force
    }

    Push-Location $apiDir
    try {
        & go build ./...
        if ($LASTEXITCODE -ne 0) { throw '后端编译失败（go build ./...）' }
    }
    finally {
        Pop-Location
    }
}
finally {
    Pop-Location
}

Write-Output 'build-all 完成：前端构建 -> 产物嵌入 -> go build ./...'