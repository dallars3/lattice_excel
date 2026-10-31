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

Write-Output ('Generated protobuf code in ' + (Join-Path $Root 'gen'))
