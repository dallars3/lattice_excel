# contract-test.ps1：数量一致性、三层契约清单与三服务串行回归。
param([string]$Root = '')

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($Root)) {
  $top = (& git rev-parse --show-toplevel 2>$null)
  if ($top) {
    $Root = $top.Trim()
  } else {
    $Root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
  }
}
$Root = (Resolve-Path -LiteralPath $Root).Path
$utf8 = New-Object System.Text.UTF8Encoding($false)
$env:GOPROXY = 'https://goproxy.cn,direct'
$env:GOCACHE = Join-Path $Root '.gocache'

$script:checkCount = 0
$script:failures = 0
$script:gaps = New-Object System.Collections.Generic.List[string]

function Add-Check {
  param([bool]$Ok, [string]$Message)
  $script:checkCount++
  if ($Ok) {
    Write-Output ('PASS  ' + $Message)
  } else {
    $script:failures++
    Write-Output ('FAIL  ' + $Message)
  }
}

function Add-Gap {
  param([string]$Message)
  $script:gaps.Add($Message)
  Write-Output ('GAP   ' + $Message)
}

function Get-SectionBounds {
  param(
    [string[]]$Lines,
    [string]$StartPattern,
    [string]$EndPattern
  )
  $start = -1
  $end = $Lines.Count
  for ($index = 0; $index -lt $Lines.Count; $index++) {
    if ($start -lt 0 -and $Lines[$index] -match $StartPattern) {
      $start = $index
      continue
    }
    if ($start -ge 0 -and $Lines[$index] -match $EndPattern) {
      $end = $index
      break
    }
  }
  if ($start -lt 0) {
    throw ('section start not found: ' + $StartPattern)
  }
  return @($start, $end)
}

function Get-HttpRows {
  param([string[]]$Lines, [int]$Start, [int]$End)
  $rows = New-Object System.Collections.Generic.List[object]
  for ($index = $Start; $index -lt $End; $index++) {
    if ($Lines[$index] -match '^\|\s*(H-\d{2})\s*\|\s*([A-Z]+)\s+([^|\s]+)') {
      $rows.Add([pscustomobject]@{
        Id = $matches[1]
        Method = $matches[2]
        Path = $matches[3]
        Line = $index + 1
      })
    }
  }
  return $rows.ToArray()
}

function Get-WsRows {
  param([string[]]$Lines, [int]$Start, [int]$End)
  $rows = New-Object System.Collections.Generic.List[object]
  for ($index = $Start; $index -lt $End; $index++) {
    if ($Lines[$index] -match '^\|\s*(W-\d{2})(?:\s*\|\s*|\s+)([a-z_]+)\s*\|') {
      $rows.Add([pscustomobject]@{
        Id = $matches[1]
        Name = $matches[2]
        Line = $index + 1
      })
    }
  }
  return $rows.ToArray()
}

function Get-IdRows {
  param([string[]]$Lines, [int]$Start, [int]$End, [string]$Prefix)
  $pattern = '^\|\s*(' + [regex]::Escape($Prefix) + '-\d{2})\s*\|'
  $rows = New-Object System.Collections.Generic.List[object]
  for ($index = $Start; $index -lt $End; $index++) {
    if ($Lines[$index] -match $pattern) {
      $rows.Add([pscustomobject]@{
        Id = $matches[1]
        Line = $index + 1
      })
    }
  }
  return $rows.ToArray()
}

function Assert-IdSequence {
  param($Rows, [string]$Prefix, [int]$Expected)
  $ids = @($Rows | ForEach-Object { $_.Id })
  $expectedIds = @(1..$Expected | ForEach-Object { '{0}-{1:D2}' -f $Prefix, $_ })
  $missing = @($expectedIds | Where-Object { $ids -notcontains $_ })
  $unexpected = @($ids | Where-Object { $expectedIds -notcontains $_ })
  $duplicates = @($ids | Group-Object | Where-Object { $_.Count -gt 1 })
  $ok = $ids.Count -eq $Expected -and
    $missing.Count -eq 0 -and
    $unexpected.Count -eq 0 -and
    $duplicates.Count -eq 0
  Add-Check $ok ('数量一致性 ' + $Prefix + ' ID = ' + $ids.Count + '/' + $Expected)
  foreach ($id in $missing) {
    Write-Output ('FAIL  数量一致性缺失 ' + $id + '（文档无对应行）')
  }
  foreach ($id in $unexpected) {
    $row = $Rows | Where-Object { $_.Id -eq $id } | Select-Object -First 1
    Write-Output ('FAIL  数量一致性出现非预期 ID ' + $id + '（文档:' + $row.Line + '）')
  }
  foreach ($duplicate in $duplicates) {
    $lines = @($Rows | Where-Object { $_.Id -eq $duplicate.Name } | ForEach-Object { $_.Line })
    Write-Output ('FAIL  数量一致性重复 ID ' + $duplicate.Name + '（文档行 ' + ($lines -join ',') + '）')
  }
}

function Normalize-Route {
  param([string]$Route)
  if ($Route -notmatch '^([A-Z]+)\s+(.+)$') {
    return $Route.Trim()
  }
  $method = $matches[1]
  $path = $matches[2].Split('?')[0]
  $path = [regex]::Replace($path, '\{[^}]+\}', '{}')
  return $method.ToUpperInvariant() + ' ' + $path
}

function Invoke-SpotCheck {
  param(
    [string]$Id,
    [string]$Service,
    [string[]]$Packages,
    [string]$Pattern
  )
  $serviceRoot = Join-Path $Root $Service
  Push-Location $serviceRoot
  try {
    $output = @(& go test -p 1 @Packages -run $Pattern -count=1 2>&1)
    $exitCode = $LASTEXITCODE
  } finally {
    Pop-Location
  }
  Add-Check ($exitCode -eq 0) ($Id + ' 过滤测试 ' + $Service + ' ' + ($Packages -join ' ') + ' ' + $Pattern)
  if ($exitCode -ne 0) {
    $output | ForEach-Object { Write-Output ([string]$_) }
  }
}

function Invoke-ServiceSuite {
  param([string]$Service, [string]$RelativeRoot)
  $serviceRoot = Join-Path $Root $RelativeRoot
  $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
  Push-Location $serviceRoot
  try {
    $raw = @(& go test -json -p 1 ./... -count=1 2>&1)
    $exitCode = $LASTEXITCODE
  } finally {
    Pop-Location
    $stopwatch.Stop()
  }

  $packageResults = @{}
  $outputByTest = @{}
  $failedTests = @{}
  foreach ($line in $raw) {
    try {
      $event = [string]$line | ConvertFrom-Json -ErrorAction Stop
    } catch {
      continue
    }
    $package = [string]$event.Package
    if ([string]::IsNullOrWhiteSpace($package)) {
      continue
    }
    $test = ''
    if ($event.PSObject.Properties.Name -contains 'Test') {
      $test = [string]$event.Test
    }
    $action = [string]$event.Action
    if ($action -eq 'output') {
      $key = $package + [char]31 + $test
      if (-not $outputByTest.ContainsKey($key)) {
        $outputByTest[$key] = New-Object System.Collections.Generic.List[string]
      }
      $outputByTest[$key].Add([string]$event.Output)
      continue
    }
    if ($action -in @('pass', 'fail', 'skip') -and $test -ne '') {
      if ($action -eq 'fail') {
        $failedTests[$package + [char]31 + $test] = [pscustomobject]@{
          Package = $package
          Test = $test
        }
      }
      continue
    }
    if ($action -in @('pass', 'fail') -and $test -eq '') {
      $elapsed = 0.0
      if ($event.PSObject.Properties.Name -contains 'Elapsed') {
        $elapsed = [double]$event.Elapsed
      }
      $packageResults[$package] = [pscustomobject]@{
        Status = $action
        Elapsed = $elapsed
      }
    }
  }

  $detailLines = New-Object System.Collections.Generic.List[string]
  $sortedPackages = @($packageResults.Keys | Sort-Object)
  foreach ($package in $sortedPackages) {
    $result = $packageResults[$package]
    $detailLines.Add(('{0,-4} 服务={1} 包={2} 耗时={3:N2}s' -f
      $result.Status.ToUpperInvariant(),
      $Service,
      $package,
      $result.Elapsed))
  }

  foreach ($failed in $failedTests.Values) {
    $key = $failed.Package + [char]31 + $failed.Test
    $combined = ''
    if ($outputByTest.ContainsKey($key)) {
      $combined = $outputByTest[$key] -join ''
    }
    $meaningful = @(
      $combined -split "`r?`n" |
        ForEach-Object { $_.Trim() } |
        Where-Object {
          $_ -and
          $_ -notmatch '^=== (RUN|PAUSE|CONT)' -and
          $_ -notmatch '^--- (PASS|FAIL)' -and
          $_ -notmatch '^(PASS|FAIL)$'
        }
    )
    $assertion = '测试进程返回失败'
    if ($meaningful.Count -gt 0) {
      $assertion = $meaningful[$meaningful.Count - 1]
    }
    $expected = '见断言中的 want/expected'
    if ($assertion -match '(?:want|expected)\s+(.+)$') {
      $expected = $matches[1]
    }
    $actual = '测试失败'
    if ($assertion -match '^(.*?)(?:,\s*want\b|\s+want\b)') {
      $actual = $matches[1]
    }
    $detailLines.Add(('FAIL 服务={0} 包={1} 测试={2} 断言={3} 期望={4} 实际={5}' -f
      $Service,
      $failed.Package,
      $failed.Test,
      $assertion,
      $expected,
      $actual))
  }

  if ($exitCode -ne 0 -and $failedTests.Count -eq 0) {
    foreach ($package in $sortedPackages) {
      if ($packageResults[$package].Status -ne 'fail') {
        continue
      }
      $key = $package + [char]31
      $combined = ''
      if ($outputByTest.ContainsKey($key)) {
        $combined = $outputByTest[$key] -join ''
      }
      $meaningful = @(
        $combined -split "`r?`n" |
          ForEach-Object { $_.Trim() } |
          Where-Object { $_ -and $_ -notmatch '^(FAIL|PASS)$' }
      )
      $assertion = '包级构建或测试失败'
      if ($meaningful.Count -gt 0) {
        $assertion = $meaningful[$meaningful.Count - 1]
      }
      $detailLines.Add(('FAIL 服务={0} 包={1} 测试=<package> 断言={2} 期望=exit 0 实际=exit {3}' -f
        $Service,
        $package,
        $assertion,
        $exitCode))
    }
  }

  $passed = @($packageResults.Values | Where-Object { $_.Status -eq 'pass' }).Count
  $total = $packageResults.Count
  $failed = @($packageResults.Values | Where-Object { $_.Status -eq 'fail' }).Count
  return [pscustomobject]@{
    Service = $Service
    ExitCode = $exitCode
    Passed = $passed
    Failed = $failed
    Total = $total
    Elapsed = $stopwatch.Elapsed.TotalSeconds
    DetailLines = $detailLines.ToArray()
  }
}

Write-Output '== 契约事实源数量一致性 =='
$docPath = Join-Path $Root '接口文档-v0.6.md'
if (-not (Test-Path -LiteralPath $docPath)) {
  Add-Check $false ('缺少事实源 ' + $docPath)
  exit 1
}
$docLines = [System.IO.File]::ReadAllLines($docPath, $utf8)

$bounds = @(Get-SectionBounds $docLines '^## 2\.' '^## 3\.')
$httpRows = @(Get-HttpRows $docLines $bounds[0] $bounds[1])
$bounds = @(Get-SectionBounds $docLines '^### 8\.1 ' '^### 8\.2 ')
$httpSpecRows = @(Get-IdRows $docLines $bounds[0] $bounds[1] 'H')
Assert-IdSequence $httpRows 'H' 93
Assert-IdSequence $httpSpecRows 'H' 93
$httpIds = @($httpRows | ForEach-Object { $_.Id })
$httpSpecIds = @($httpSpecRows | ForEach-Object { $_.Id })
Add-Check (($httpIds -join ',') -eq ($httpSpecIds -join ',')) '§2 与 §8.1 的 H ID 顺序一致'

$bounds = @(Get-SectionBounds $docLines '^## 3\.' '^## 4\.')
$wsRows = @(Get-WsRows $docLines $bounds[0] $bounds[1])
$bounds = @(Get-SectionBounds $docLines '^### 8\.2 ' '^### 8\.3 ')
$wsSpecRows = @(Get-WsRows $docLines $bounds[0] $bounds[1])
Assert-IdSequence $wsRows 'W' 16
Assert-IdSequence $wsSpecRows 'W' 16
$wsIds = @($wsRows | ForEach-Object { $_.Id })
$wsSpecIds = @($wsSpecRows | ForEach-Object { $_.Id })
Add-Check (($wsIds -join ',') -eq ($wsSpecIds -join ',')) '§3 与 §8.2 的 W ID 顺序一致'

$bounds = @(Get-SectionBounds $docLines '^## 4\.' '^## 5\.')
$grpcDocRows = New-Object System.Collections.Generic.List[object]
for ($index = $bounds[0]; $index -lt $bounds[1]; $index++) {
  if ($docLines[$index] -match '^\|\s*([A-Z][A-Za-z0-9]+)\s*\|') {
    $grpcDocRows.Add([pscustomobject]@{
      Method = $matches[1]
      Line = $index + 1
    })
  }
}
$protoRows = New-Object System.Collections.Generic.List[object]
$protoRoot = Join-Path $Root 'proto'
Get-ChildItem -LiteralPath $protoRoot -Recurse -Filter '*.proto' | ForEach-Object {
  $protoFile = $_
  Select-String -LiteralPath $protoFile.FullName -Pattern '^\s*rpc\s+' | ForEach-Object {
    $protoRows.Add([pscustomobject]@{
      File = $protoFile.FullName
      Line = $_.LineNumber
      Text = $_.Line.Trim()
    })
  }
}
Add-Check ($grpcDocRows.Count -eq 111) ('§4 gRPC 方法 = ' + $grpcDocRows.Count + '/111')
Add-Check ($protoRows.Count -eq 111) ('proto rpc = ' + $protoRows.Count + '/111')
if ($grpcDocRows.Count -ne 111) {
  Write-Output ('FAIL  §4 gRPC 行号 ' + (($grpcDocRows | ForEach-Object { $_.Line }) -join ','))
}
if ($protoRows.Count -ne 111) {
  $locations = @($protoRows | ForEach-Object { $_.File + ':' + $_.Line })
  Write-Output ('FAIL  proto rpc 行号 ' + ($locations -join ','))
}

$bounds = @(Get-SectionBounds $docLines '^## 附录：接口数量速览' '^__NO_SUCH_HEADING__$')
$appendix = @{}
for ($index = $bounds[0]; $index -lt $docLines.Count; $index++) {
  if ($docLines[$index] -match '^\|\s*(HTTP 接口（H）|WebSocket 消息（W）|gRPC 接口（G）)\s*\|\s*(\d+)\s*\|') {
    $appendix[$matches[1]] = [pscustomobject]@{
      Count = [int]$matches[2]
      Line = $index + 1
    }
  }
}
foreach ($expected in @(
  [pscustomobject]@{ Name = 'HTTP 接口（H）'; Count = 93 },
  [pscustomobject]@{ Name = 'WebSocket 消息（W）'; Count = 16 },
  [pscustomobject]@{ Name = 'gRPC 接口（G）'; Count = 111 }
)) {
  $actual = 0
  if ($appendix.ContainsKey($expected.Name)) {
    $actual = $appendix[$expected.Name].Count
  }
  $line = 'missing'
  if ($appendix.ContainsKey($expected.Name)) {
    $line = [string]$appendix[$expected.Name].Line
  }
  Add-Check ($actual -eq $expected.Count) (
    '§11 附录 ' + $expected.Name + ' = ' + $actual + '/' + $expected.Count + '（文档:' + $line + '）'
  )
}
Write-Output ('SUMMARY 数量 H=' + $httpRows.Count + ' W=' + $wsRows.Count +
  ' gRPC(doc)=' + $grpcDocRows.Count + ' gRPC(proto)=' + $protoRows.Count)

Write-Output '== HTTP Routes() 运行时清单 =='
$webRoot = Join-Path $Root 'excel_web_api'
Push-Location $webRoot
try {
  $inventoryOutput = @(& go test ./internal/gateway -run '^TestContractHTTPRouteInventory$' -count=1 -v 2>&1)
  $inventoryExit = $LASTEXITCODE
} finally {
  Pop-Location
}
Add-Check ($inventoryExit -eq 0) 'HTTP Routes() 清单测试可执行'
if ($inventoryExit -ne 0) {
  $inventoryOutput | ForEach-Object { Write-Output ([string]$_) }
}
$actualRoutes = New-Object System.Collections.Generic.List[string]
foreach ($line in $inventoryOutput) {
  if (([string]$line) -match 'CONTRACT_ROUTE\s+((GET|POST|PUT|PATCH|DELETE)\s+\S+)') {
    $actualRoutes.Add($matches[1])
  }
}
Add-Check ($actualRoutes.Count -gt 0) ('HTTP 已注册运行时路由 = ' + $actualRoutes.Count)
$actualRouteKeys = @($actualRoutes | ForEach-Object { Normalize-Route $_ })
$docRouteMap = @{}
foreach ($row in $httpRows) {
  $key = Normalize-Route ($row.Method + ' ' + $row.Path)
  if (-not $docRouteMap.ContainsKey($key)) {
    $docRouteMap[$key] = @()
  }
  $docRouteMap[$key] += $row
}
$matchedRows = @($httpRows | Where-Object {
  $actualRouteKeys -contains (Normalize-Route ($_.Method + ' ' + $_.Path))
})
$extraRoutes = @($actualRouteKeys | Where-Object { -not $docRouteMap.ContainsKey($_) } | Sort-Object -Unique)
Write-Output ('SUMMARY HTTP 已注册=' + $actualRoutes.Count + '/93 文档匹配=' +
  $matchedRows.Count + '/93 未注册=' + (93 - $matchedRows.Count) +
  ' 额外别名=' + $extraRoutes.Count)
foreach ($row in $httpRows) {
  $key = Normalize-Route ($row.Method + ' ' + $row.Path)
  if ($actualRouteKeys -notcontains $key) {
    Add-Gap ('HTTP ' + $row.Id + ' ' + $row.Method + ' ' + $row.Path + ' 未注册（文档:' + $row.Line + '）')
  }
}
foreach ($route in $extraRoutes) {
  Add-Gap ('HTTP 额外已注册路由 ' + $route + ' 不在 §2 清单')
}

Write-Output '== WebSocket Frame 清单 =='
$generatedPath = Join-Path $Root 'gen\api\webapi\v1\webapi.pb.go'
Add-Check (Test-Path -LiteralPath $generatedPath) '生成 WebSocket Frame 契约存在'
$framePayloadVariants = 0
if (Test-Path -LiteralPath $generatedPath) {
  $framePayloadVariants = @(
    Select-String -LiteralPath $generatedPath -Pattern 'func \(\*Frame_[A-Za-z]+\) isFrame_Payload\(\)'
  ).Count
}
$hubPath = Join-Path $Root 'excel_web_api\internal\realtime\hub.go'
$hubText = [System.IO.File]::ReadAllText($hubPath, $utf8)
$dispatchStart = $hubText.IndexOf('func (hub *Hub) dispatch')
$joinStart = $hubText.IndexOf('func (hub *Hub) join')
$inboundCases = 0
if ($dispatchStart -ge 0 -and $joinStart -gt $dispatchStart) {
  $dispatchText = $hubText.Substring($dispatchStart, $joinStart - $dispatchStart)
  $inboundCases = @([regex]::Matches($dispatchText, 'case\s+\*webapiv1\.Frame_[A-Za-z]+')).Count
}
Add-Check ($framePayloadVariants -eq 16) ('WebSocket Frame payload = ' + $framePayloadVariants + '/16')
$realtimeText = (
  Get-ChildItem -LiteralPath (Join-Path $Root 'excel_web_api\internal\realtime') -Filter '*.go' |
    ForEach-Object { [System.IO.File]::ReadAllText($_.FullName, $utf8) }
) -join "`n"
$missingWs = @($wsRows | Where-Object {
  $realtimeText.IndexOf($_.Name, [StringComparison]::Ordinal) -lt 0
})
Add-Check ($missingWs.Count -eq 0) ('WS 消息名称源码/测试存在 = ' + (16 - $missingWs.Count) + '/16')
foreach ($row in $missingWs) {
  Add-Gap ('WS ' + $row.Id + ' ' + $row.Name + ' 未在 realtime 源码/测试中出现（文档:' + $row.Line + '）')
}
Write-Output ('SUMMARY WS payload=' + $framePayloadVariants + '/16 inbound_hub_cases=' +
  $inboundCases + ' message_presence=' + (16 - $missingWs.Count) + '/16')

Write-Output '== C-01..C-16 覆盖矩阵 =='
$coveragePath = Join-Path $Root 'docs\tools\contract-coverage.md'
Add-Check (Test-Path -LiteralPath $coveragePath) '契约覆盖矩阵存在'
$coverageRows = New-Object System.Collections.Generic.List[object]
if (Test-Path -LiteralPath $coveragePath) {
  $coverageLines = [System.IO.File]::ReadAllLines($coveragePath, $utf8)
  for ($index = 0; $index -lt $coverageLines.Count; $index++) {
    if ($coverageLines[$index] -match '^\|\s*(C-\d{2})\s*\|') {
      $cells = @(
        $coverageLines[$index].Trim().Trim('|').Split('|') |
          ForEach-Object { $_.Trim() }
      )
      $coverageRows.Add([pscustomobject]@{
        Id = $matches[1]
        Cells = $cells
        Line = $index + 1
      })
    }
  }
}
Assert-IdSequence $coverageRows 'C' 16
foreach ($row in $coverageRows) {
  $shapeOk = $row.Cells.Count -eq 7
  $assertionOk = $row.Cells.Count -ge 3 -and -not [string]::IsNullOrWhiteSpace($row.Cells[2])
  $status = ''
  if ($row.Cells.Count -ge 6) {
    $status = $row.Cells[5]
  }
  $statusOk = $status -in @('PASS', 'GAP')
  $noteOk = $status -ne 'GAP' -or
    ($row.Cells.Count -ge 7 -and -not [string]::IsNullOrWhiteSpace($row.Cells[6]))
  Add-Check ($shapeOk -and $assertionOk -and $statusOk -and $noteOk) (
    $row.Id + ' 矩阵列/断言归属/状态/缺口原因完整（文档:' + $row.Line + '）'
  )
  if ($status -eq 'GAP') {
    Add-Gap ($row.Id + ' ' + $row.Cells[1] + '：' + $row.Cells[6])
  }
}
$coveragePass = @($coverageRows | Where-Object { $_.Cells.Count -ge 6 -and $_.Cells[5] -eq 'PASS' }).Count
$coverageGap = @($coverageRows | Where-Object { $_.Cells.Count -ge 6 -and $_.Cells[5] -eq 'GAP' }).Count
Write-Output ('SUMMARY C PASS=' + $coveragePass + ' GAP=' + $coverageGap + ' total=' + $coverageRows.Count)

Write-Output '== 六个 C 项过滤抽查 =='
Invoke-SpotCheck 'C-01' 'excel_web_api' @('./internal/gateway') `
  '^TestProtectedRoutesRejectMissingSessionBeforeAuthorizationOrBusiness$'
Invoke-SpotCheck 'C-03' 'excel_core_service' @('./internal/iam') `
  '^(TestRegisterRejectsCaseInsensitiveDuplicateAndInvalidCredentials|TestChangePasswordRequiresOldPasswordAndNewPasswordPolicy)$'
Invoke-SpotCheck 'C-05' 'excel_worker_service' @('./internal/notify') `
  '^TestNotifyRPCsPaginateRetainAndMarkReadIdempotently$'
Invoke-SpotCheck 'C-06' 'excel_core_service' @('./internal/records') `
  '^TestCreateRecordIsIdempotentForTwentyFourHoursAndForcesCreator$'
Invoke-SpotCheck 'C-09' 'excel_web_api' @('./internal/gateway', './redis') `
  '^(TestPipelineRateLimitsSensitiveRoutesWith5003|TestPipelineDoesNotRateLimitUnrelatedRoutes|TestRateLimiterSlidingWindowRejectsExcessRequests)$'
Invoke-SpotCheck 'C-14' 'excel_web_api' @('./internal/gateway') `
  '^TestExportRouteRejectsExternalCollaboratorBeforeBusinessRPC$'

Write-Output '== 三服务串行全量回归 =='
$suiteResults = New-Object System.Collections.Generic.List[object]
foreach ($suite in @(
  [pscustomobject]@{ Name = 'core'; Root = 'excel_core_service' },
  [pscustomobject]@{ Name = 'worker'; Root = 'excel_worker_service' },
  [pscustomobject]@{ Name = 'web'; Root = 'excel_web_api' }
)) {
  Write-Output ('RUN   服务=' + $suite.Name + ' go test -json -p 1 ./... -count=1')
  $result = Invoke-ServiceSuite $suite.Name $suite.Root
  $suiteResults.Add($result)
  foreach ($detail in $result.DetailLines) {
    Write-Output $detail
  }
  $rate = 0.0
  if ($result.Total -gt 0) {
    $rate = 100.0 * $result.Passed / $result.Total
  }
  $ok = $result.ExitCode -eq 0 -and $result.Failed -eq 0
  Add-Check $ok ('服务=' + $suite.Name + ' pass=' + $result.Passed + '/' + $result.Total +
    ' rate=' + ('{0:N1}' -f $rate) + '% elapsed=' + ('{0:N2}' -f $result.Elapsed) + 's')
  Write-Output ('RUN   服务=' + $suite.Name + ' go vet ./...')
  Push-Location (Join-Path $Root $suite.Root)
  try {
    $vetOutput = @(& go vet ./... 2>&1)
    $vetExit = $LASTEXITCODE
  }
  finally {
    Pop-Location
  }
  foreach ($vetLine in $vetOutput) {
    if (-not [string]::IsNullOrWhiteSpace([string]$vetLine)) {
      Write-Output $vetLine
    }
  }
  Add-Check ($vetExit -eq 0) ('服务=' + $suite.Name + ' go vet ./...')
}

$totalPassed = 0
$totalPackages = 0
$totalElapsed = 0.0
foreach ($result in $suiteResults) {
  $totalPassed += $result.Passed
  $totalPackages += $result.Total
  $totalElapsed += $result.Elapsed
}
$totalRate = 0.0
if ($totalPackages -gt 0) {
  $totalRate = 100.0 * $totalPassed / $totalPackages
}
Write-Output ('RESULT PASS RATE=' + $totalPassed + '/' + $totalPackages +
  ' (' + ('{0:N1}' -f $totalRate) + '%) elapsed=' + ('{0:N2}' -f $totalElapsed) + 's')
Write-Output ('RESULT GAPS=' + $script:gaps.Count)
foreach ($gap in $script:gaps) {
  Write-Output ('GAP SUMMARY ' + $gap)
}

if ($script:failures -eq 0) {
  Write-Output ('RESULT PASS checks=' + $script:checkCount + ' fail=0 gaps=' + $script:gaps.Count)
  exit 0
}
Write-Output ('RESULT FAIL checks=' + $script:checkCount + ' fail=' + $script:failures + ' gaps=' + $script:gaps.Count)
exit 1
