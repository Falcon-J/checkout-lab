param([Parameter(Mandatory=$true)][string]$DatabaseURL,[switch]$SkipRace)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path $PSScriptRoot
Push-Location $projectRoot
$previousDatabase=$env:TEST_DATABASE_URL
try{
 $env:TEST_DATABASE_URL=$DatabaseURL
 $unformatted=gofmt -l cmd internal
 if ($LASTEXITCODE -ne 0 -or $unformatted){throw "Formatting check failed: $unformatted"}
 go mod verify
 if ($LASTEXITCODE -ne 0){throw 'Module verification failed'}
 go vet ./...
 if ($LASTEXITCODE -ne 0){throw 'Go vet failed'}
 go build ./...
 if ($LASTEXITCODE -ne 0){throw 'Build/typecheck failed'}
 go test ./... -count=1 -v
 if ($LASTEXITCODE -ne 0){throw 'Tests failed'}
 if ($SkipRace) {Write-Warning 'Race check explicitly skipped; not verified'} else {
 go test -race ./... -count=1
 if ($LASTEXITCODE -ne 0){throw 'Race tests failed; a 64-bit C compiler is required on Windows'}
 }
}finally{$env:TEST_DATABASE_URL=$previousDatabase;Pop-Location}
