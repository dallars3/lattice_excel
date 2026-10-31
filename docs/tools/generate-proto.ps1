param([string]$Root = '')

$ErrorActionPreference = 'Stop'

if ($Root -eq '') {
    $top = (& git rev-parse --show-toplevel 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $top) {
        throw 'Unable to determine repository root.'
    }
    $Root = $top.Trim()
}

$env:GOPROXY = 'https://goproxy.cn,direct'
$goBin = (& go env GOBIN).Trim()
if ($LASTEXITCODE -ne 0) {
    throw 'Unable to read GOBIN.'
}
if (-not $goBin) {
    $goPath = (& go env GOPATH).Trim()
    if ($LASTEXITCODE -ne 0) {
        throw 'Unable to read GOPATH.'
    }
    $goBin = Join-Path $goPath 'bin'
}
$env:PATH = "$goBin;$env:PATH"

if (-not (Get-Command protoc -ErrorAction SilentlyContinue)) {
    throw 'protoc is required.'
}

$tools = @(
    @{ Name = 'buf'; Package = 'github.com/bufbuild/buf/cmd/buf@latest' },
    @{ Name = 'protoc-gen-go'; Package = 'google.golang.org/protobuf/cmd/protoc-gen-go@latest' },
    @{ Name = 'protoc-gen-go-grpc'; Package = 'google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest' }
)

foreach ($tool in $tools) {
    if (Get-Command $tool.Name -ErrorAction SilentlyContinue) {
        continue
    }
    & go install $tool.Package
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to install $($tool.Name)."
    }
}

Push-Location (Join-Path $Root 'proto')
try {
    & buf lint
    if ($LASTEXITCODE -ne 0) {
        throw 'buf lint failed.'
    }
    & buf generate
    if ($LASTEXITCODE -ne 0) {
        throw 'buf generate failed.'
    }
}
finally {
    Pop-Location
}

$genDir = Join-Path $Root 'gen'
$genMod = Join-Path $genDir 'go.mod'
Set-Content -LiteralPath $genMod -Encoding ascii -Value "module excel`n`ngo 1.22`n"

Push-Location $genDir
try {
    # 与三服务各自 go.mod 锁定的版本保持一致（go1.22 兼容；grpc v1.66.2 / protobuf v1.34.1）
    & go get google.golang.org/protobuf@v1.34.1 google.golang.org/grpc@v1.66.2
    if ($LASTEXITCODE -ne 0) {
        throw 'go get pinned deps failed in gen.'
    }
    & go mod tidy
    if ($LASTEXITCODE -ne 0) {
        throw 'go mod tidy failed in gen.'
    }
}
finally {
    Pop-Location
}

Write-Output ('Generated protobuf code in ' + $genDir)
