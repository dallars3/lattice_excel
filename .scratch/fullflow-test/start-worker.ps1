Start-Process -FilePath 'F:\codex\repo\lark excel\out\excel-worker-service.exe' `
  -WorkingDirectory 'F:\codex\repo\lark excel\excel_worker_service' `
  -RedirectStandardOutput 'F:\codex\repo\lark excel\out\worker-p2.out.log' `
  -RedirectStandardError 'F:\codex\repo\lark excel\out\worker-p2.err.log' `
  -NoNewWindow -Wait
