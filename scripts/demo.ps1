param(
 [string]$DatabaseURL = $env:DATABASE_URL,
 [int]$Port = 18080
)
$ErrorActionPreference='Stop'
if (!$DatabaseURL) {throw 'Set DATABASE_URL to a separate disposable demo database with the initial schema.'}
$projectRoot=Split-Path $PSScriptRoot
$binary=Join-Path $projectRoot 'bin\reservation.exe'
if (!(Test-Path -LiteralPath $binary)) {throw 'Build first: go build -o bin/reservation.exe ./cmd/reservation'}
New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot '.tmp') | Out-Null
$previousDatabase=$env:DATABASE_URL
$previousToken=$env:API_TOKEN
$env:DATABASE_URL=$DatabaseURL
$env:API_TOKEN=[Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$headers=@{Authorization="Bearer $env:API_TOKEN";'Idempotency-Key'=[guid]::NewGuid().ToString('N')}
$base="http://127.0.0.1:$Port"
$api=$null
$worker=$null
try {
 $api=Start-Process -FilePath $binary -ArgumentList @('-mode','api','-addr',"127.0.0.1:$Port",'-ttl','5s') -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $projectRoot '.tmp\demo-api.log')
 $ready=$false
 for ($attempt=0;$attempt -lt 50;$attempt++) {
  if ($api.HasExited) {throw 'API exited; inspect .tmp/demo-api.log'}
  try { $null=Invoke-RestMethod -NoProxy -TimeoutSec 3 "$base/health/ready";$ready=$true;break } catch {Start-Sleep -Milliseconds 100}
 }
 if (!$ready) {throw 'API did not become ready'}
 $worker=Start-Process -FilePath $binary -ArgumentList @('-mode','worker','-interval','100ms') -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $projectRoot '.tmp\demo-worker-before.log')
 $reservation=Invoke-RestMethod -NoProxy -TimeoutSec 3 "$base/reservations" -Method Post -Headers $headers -ContentType 'application/json' -Body '{"sku":"book-go","quantity":10}'
 $replay=Invoke-RestMethod -NoProxy -TimeoutSec 3 "$base/reservations" -Method Post -Headers $headers -ContentType 'application/json' -Body '{"sku":"book-go","quantity":10}'
 if ($reservation.id -ne $replay.id) {throw 'Replay created a second reservation'}
 if ($worker.HasExited) {throw 'Worker exited unexpectedly'}
 Stop-Process -Id $worker.Id -Force
 $worker.WaitForExit()
 Start-Sleep -Seconds 6
 $before=Invoke-RestMethod -NoProxy -TimeoutSec 3 "$base/reservations/$($reservation.id)" -Headers $headers
 if ($before.status -ne 'held') {throw 'Reservation expired before restart; repeat on a less loaded machine'}
 $worker=Start-Process -FilePath $binary -ArgumentList @('-mode','worker','-interval','100ms') -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $projectRoot '.tmp\demo-worker-after.log')
 $recovered=$false
 for ($attempt=0;$attempt -lt 50;$attempt++){
  $after=Invoke-RestMethod -NoProxy -TimeoutSec 3 "$base/reservations/$($reservation.id)" -Headers $headers
  if ($after.status -eq 'expired') {$recovered=$true;break}
  Start-Sleep -Milliseconds 100
 }
 if (!$recovered) {throw 'Restart did not recover expiry'}
 $headers['Idempotency-Key']=[guid]::NewGuid().ToString('N')
 $second=Invoke-RestMethod -NoProxy -TimeoutSec 3 "$base/reservations" -Method Post -Headers $headers -ContentType 'application/json' -Body '{"sku":"book-go","quantity":10}'
 if ($second.status -ne 'held') {throw 'Released stock could not be reserved'}
 Write-Output 'PASS: same-key replay; killed worker retained due state; restarted worker expired it; all ten stock units became reservable.'
} finally {
 foreach ($ownedProcess in @($api,$worker)){
  if ($ownedProcess -and !$ownedProcess.HasExited){Stop-Process -Id $ownedProcess.Id -Force;$ownedProcess.WaitForExit()}
 }
 $env:DATABASE_URL=$previousDatabase
 $env:API_TOKEN=$previousToken
}
