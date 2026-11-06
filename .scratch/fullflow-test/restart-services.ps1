$ErrorActionPreference = 'SilentlyContinue'
$root = 'F:\codex\repo\lark excel'
Get-Process excel-web-api, excel-worker-service, excel-core-service | Stop-Process -Force
Start-Sleep -Seconds 3
Start-Process -FilePath "$root\out\excel-core-service.exe" -WorkingDirectory "$root\excel_core_service" `
  -RedirectStandardOutput "$root\out\core-p2.out.log" -RedirectStandardError "$root\out\core-p2.err.log" -NoNewWindow
Start-Sleep -Seconds 5
Start-Process -FilePath "$root\out\excel-worker-service.exe" -WorkingDirectory "$root\excel_worker_service" `
  -RedirectStandardOutput "$root\out\worker-p2.out.log" -RedirectStandardError "$root\out\worker-p2.err.log" -NoNewWindow
Start-Sleep -Seconds 5
Start-Process -FilePath "$root\out\excel-web-api.exe" -WorkingDirectory "$root\excel_web_api" `
  -RedirectStandardOutput "$root\out\web-p2.out.log" -RedirectStandardError "$root\out\web-p2.err.log" -NoNewWindow
