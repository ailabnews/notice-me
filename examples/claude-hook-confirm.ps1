$cmd = $env:CLAUDE_TOOL_INPUT
if (-not $cmd) { $cmd = '(no tool input env)' }
try {
  $r = Invoke-RestMethod -Method Post -Uri http://127.0.0.1:1886/api/confirm -Body $cmd -TimeoutSec 200
} catch {
  $r = 'denied'
}
if ($r -eq 'approved') { exit 0 } else { Write-Error "用户拒绝: $r"; exit 2 }
