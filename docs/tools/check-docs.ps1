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

$mds = Get-ChildItem -LiteralPath $Root -Recurse -Filter *.md
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
Get-ChildItem -LiteralPath $Root -Recurse -File | Where-Object { $_.Extension -in @('.md','.yaml','.yml','.go') } | ForEach-Object {
  $t = [System.IO.File]::ReadAllText($_.FullName, $enc)
  $lines2 = $t -split "`r?`n", 0
  foreach ($w in $legacy) { foreach ($ln in $lines2) { if ($ln.Contains($w) -and -not $ln.Contains('弃用')) { $hits++; Write-Output ('FAIL  旧词 ' + $w + ' : ' + $_.FullName + ' (弃用说明行已豁免)') } } }
}
Say ($hits -eq 0) '旧词/旧版本引用残留'

$rpcs = (Get-ChildItem -LiteralPath (Join-Path $Root 'proto') -Recurse -Filter *.proto | Select-String -Pattern '^\s*rpc ').Count
Say ($rpcs -eq 110) ('proto RPC 计数 = ' + $rpcs + '（期望 110）')

$tickets = Get-ChildItem -LiteralPath (Join-Path $Root '.scratch') -Recurse -Filter *.md
$noStatus = ($tickets | Where-Object { -not ([System.IO.File]::ReadAllText($_.FullName, $enc) -match 'Status:') }).Count
Say ($noStatus -eq 0) ('scratch 票/规格 Status 行齐全（缺 ' + $noStatus + '）')

Write-Output ('检查结束：fail=' + $fail)
exit $fail