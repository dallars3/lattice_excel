# check-docs.ps1：仓库文档护栏（编码/围栏/控制字符/旧词残留/RPC 计数/票状态）。
# 用法：powershell -File docs\tools\check-docs.ps1
param([string]$Root = '')
if ($Root -eq '') {
  $top = (& git rev-parse --show-toplevel 2>$null)
  if ($top) { $Root = $top.Trim() } else { $Root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path }
}
$ErrorActionPreference = 'Continue'
$enc = New-Object System.Text.UTF8Encoding($false)
$fail = 0
function Say($ok, $msg) { Write-Output (($(if ($ok) {'PASS'} else {'FAIL'})) + '  ' + $msg); if (-not $ok) { $script:fail++ } }

# 第三方依赖目录（node_modules）内的 md/yaml/go 不属于仓库文档，跳过扫描（web/ 引入前端工程后新增）。
$skipRe = '[\\/]node_modules[\\/]'
$mds = Get-ChildItem -LiteralPath $Root -Recurse -Filter *.md | Where-Object { $_.FullName -notmatch $skipRe }
foreach ($f in $mds) {
  $bytes = [System.IO.File]::ReadAllBytes($f.FullName)
  $bom = ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF)
  if ($bom) { Write-Output ('FAIL  BOM: ' + $f.FullName); $fail++ }
  $txt = [System.IO.File]::ReadAllText($f.FullName, $enc)
  $lone = 0; for ($i = 0; $i -lt $txt.Length; $i++) { if ($txt[$i] -eq [char]10 -and ($i -eq 0 -or $txt[$i-1] -ne [char]13)) { $lone++ } }
  if ($lone -gt 0) { Write-Output ('FAIL  loneLF(' + $lone + '): ' + $f.FullName); $fail++ }
  if ($txt.Contains([char]8) -or $txt.Contains([char]27)) { Write-Output ('FAIL  控制字符: ' + $f.FullName); $fail++ }
  $lines = $txt -split "`r?`n", 0
  $open = ($lines | Where-Object { $_ -match '^```' }).Count
  if ($open % 2 -ne 0) { Write-Output ('FAIL  围栏不配对: ' + $f.FullName); $fail++ }
}
Say ($fail -eq 0) ('md 编码/围栏：检查 ' + $mds.Count + ' 个文件')

$legacy = @('OperatorResetPassword','ForceCloseByAdmin','LinkCtx','by_user','excel-permission-config','整体技术方案 v1.2','接口文档 v0.5')
$hits = 0
Get-ChildItem -LiteralPath $Root -Recurse -File | Where-Object { $_.Extension -in @('.md','.yaml','.yml','.go') -and $_.FullName -notmatch $skipRe } | ForEach-Object {
  $t = [System.IO.File]::ReadAllText($_.FullName, $enc)
  $lines2 = $t -split "`r?`n", 0
  foreach ($w in $legacy) { foreach ($ln in $lines2) { if ($ln.Contains($w) -and -not $ln.Contains('弃用')) { $hits++; Write-Output ('FAIL  旧词 ' + $w + ' : ' + $_.FullName + ' (弃用说明行已豁免)') } } }
}
Say ($hits -eq 0) '旧词/旧版本引用残留'

$rpcs = (Get-ChildItem -LiteralPath (Join-Path $Root 'proto') -Recurse -Filter *.proto | Select-String -Pattern '^\s*rpc ').Count
Say ($rpcs -eq 111) ('proto RPC 计数 = ' + $rpcs + '（期望 111）')

$bufExe = $null
$bufCmd = Get-Command buf -ErrorAction SilentlyContinue
if ($bufCmd) { $bufExe = $bufCmd.Source }
if (-not $bufExe) {
  $goBin = (& go env GOPATH).Trim()
  if ($goBin) {
    $candidate = Join-Path $goBin 'bin\buf.exe'
    if (Test-Path -LiteralPath $candidate) { $bufExe = $candidate }
  }
}
if ($bufExe) {
  Push-Location (Join-Path $Root 'proto')
  try {
    & $bufExe build 2>&1 | Out-Null
    $bufExit = $LASTEXITCODE
  }
  finally {
    Pop-Location
  }
  Say ($bufExit -eq 0) 'proto 可编译（buf build）'
} else {
  Say $false 'proto 可编译（buf 未安装；先跑 docs/tools/generate-proto.ps1）'
}

$migDirs = @('excel_core_service', 'excel_worker_service')
$seenPrefixes = @{}
$seenTables = @{}
$migProblems = @()
foreach ($service in $migDirs) {
  $migPath = Join-Path $Root (Join-Path $service 'base\migrations')
  if (-not (Test-Path -LiteralPath $migPath)) { continue }
  Get-ChildItem -LiteralPath $migPath -Filter *.sql | ForEach-Object {
    $name = $_.BaseName
    if ($name -notmatch '^([0-9]{6})_[a-z0-9_]+\.(up|down)$') {
      $migProblems += ('非法迁移名: ' + $_.FullName)
      return
    }
    $prefix = $Matches[1]
    $kind = $Matches[2]
    $scopeKey = $service + ':' + $prefix
    if ($seenPrefixes.ContainsKey($scopeKey) -and $seenPrefixes[$scopeKey] -notcontains $kind) {
      $seenPrefixes[$scopeKey] += $kind
    }
    elseif (-not $seenPrefixes.ContainsKey($scopeKey)) {
      $seenPrefixes[$scopeKey] = @($kind)
    }
    if ($kind -eq 'up') {
      $content = [System.IO.File]::ReadAllText($_.FullName, $enc)
      foreach ($match in [regex]::Matches($content, 'CREATE TABLE\s+(IF NOT EXISTS\s+)?`?([a-z0-9_]+)`?')) {
        $table = $match.Groups[2].Value
        if ($seenTables.ContainsKey($table)) {
          $migProblems += ('表名重复: ' + $table + ' (' + $_.FullName + ')')
        } else {
          $seenTables[$table] = $_.FullName
        }
      }
    }
  }
}
foreach ($entry in $seenPrefixes.GetEnumerator()) {
  if ($entry.Value.Count -ne 2) {
    $migProblems += ('迁移对不齐: ' + $entry.Key + ' 有 ' + ($entry.Value -join ','))
  }
}
Say ($migProblems.Count -eq 0) ('迁移编号唯一/up-down 成对/表名全局唯一')
foreach ($problem in $migProblems) { Write-Output ('FAIL  ' + $problem) }

$tickets = Get-ChildItem -LiteralPath (Join-Path $Root '.scratch') -Recurse -Filter *.md
$noStatus = ($tickets | Where-Object { -not ([System.IO.File]::ReadAllText($_.FullName, $enc) -match 'Status:') }).Count
Say ($noStatus -eq 0) ('scratch 票/规格 Status 行齐全（缺 ' + $noStatus + '）')

Write-Output ('检查结束：fail=' + $fail)
exit $fail
