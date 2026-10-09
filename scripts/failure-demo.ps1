param([Parameter(Mandatory=$true)][string]$DatabaseURL,[int]$Port=18081)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path $PSScriptRoot
Push-Location $projectRoot
$oldDatabase=$env:DATABASE_URL
$oldToken=$env:API_TOKEN
$ownedApi=$null
$databaseStopped=$false
try {
 $expected='postgres://reservation:local-test-only@127.0.0.1:54329/checkout_demo_'
 if (!$DatabaseURL.StartsWith($expected)) {throw 'Use a disposable checkout_demo_* database in this local Compose instance.'}
 $env:DATABASE_URL=$DatabaseURL
 $env:API_TOKEN=[Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
 $headers=@{Authorization="Bearer $env:API_TOKEN";'Idempotency-Key'=[guid]::NewGuid().ToString('N')}
 $base="http://127.0.0.1:$Port"
 $binary=Join-Path $projectRoot 'bin/reservation.exe'
 New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot '.tmp') | Out-Null
 $ownedApi=Start-Process -FilePath $binary -ArgumentList @('-addr',"127.0.0.1:$Port",'-ttl','30s','-interval','100ms') -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $projectRoot '.tmp/outage-demo.log')
 $ready=$false
 for ($i=0;$i -lt 50;$i++) {
  if ($ownedApi.HasExited) {throw 'API exited; inspect .tmp/outage-demo.log'}
  try {$null=Invoke-RestMethod -NoProxy -TimeoutSec 5 "$base/health/ready";$ready=$true;break} catch {Start-Sleep -Milliseconds 100}
 }
 if (!$ready) {throw 'API did not become ready'}
 $row=Invoke-RestMethod -NoProxy -TimeoutSec 5 "$base/reservations" -Method Post -Headers $headers -ContentType application/json -Body '{"sku":"book-go","quantity":10}'
 docker compose stop postgres
 if ($LASTEXITCODE -ne 0) {throw 'Could not stop this project database'}
 $databaseStopped=$true
 if ([DateTimeOffset]::UtcNow -ge [DateTimeOffset]::Parse([string]$row.expires_at)) {throw 'Database stopped after the hold deadline; this run cannot prove outage recovery.'}
 foreach ($path in @('/health/ready',"/reservations/$($row.id)")) {
  $response=Invoke-WebRequest -NoProxy -TimeoutSec 5 -SkipHttpErrorCheck "$base$path" -Headers $headers
  if ([int]$response.StatusCode -ne 503) {throw "Expected bounded 503 during outage, got $($response.StatusCode)"}
 }
 # Keep the database unavailable past the thirty-second hold deadline.
 Start-Sleep -Seconds 31
 docker compose up -d --wait postgres
 if ($LASTEXITCODE -ne 0) {throw 'Database restart failed'}
 $databaseStopped=$false
 $recovered=$false
 for ($i=0;$i -lt 100;$i++) {
  try {
   $got=Invoke-RestMethod -NoProxy -TimeoutSec 5 "$base/reservations/$($row.id)" -Headers $headers
   if ($got.status -eq 'expired') {$recovered=$true;break}
  } catch {}
  Start-Sleep -Milliseconds 100
 }
 if (!$recovered) {throw 'Overdue reservation did not recover after database restart'}
 $headers['Idempotency-Key']=[guid]::NewGuid().ToString('N')
 $second=Invoke-RestMethod -NoProxy -TimeoutSec 5 "$base/reservations" -Method Post -Headers $headers -ContentType application/json -Body '{"sku":"book-go","quantity":10}'
 if ($second.status -ne 'held') {throw 'Stock not restored after outage'}
 Write-Output 'PASS: database stopped; readiness and reads returned 503; database restarted; worker recovered expiry; all ten units became reservable.'
} finally {
 if ($databaseStopped) {
  docker compose up -d --wait postgres
  if ($LASTEXITCODE -ne 0) {Write-Warning 'Database restoration failed; run docker compose up -d --wait postgres.'}
 }
 if ($ownedApi -and !$ownedApi.HasExited) {Stop-Process -Id $ownedApi.Id -Force;$ownedApi.WaitForExit()}
 $env:DATABASE_URL=$oldDatabase
 $env:API_TOKEN=$oldToken
 Pop-Location
}
