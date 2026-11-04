# Standalone PS5.1 regression tests. No real Git, network, sleep, or deployment runs.
$ErrorActionPreference = 'Stop'
$errors = $null
$tokens = $null
$path = Join-Path $PSScriptRoot 'deploy.ps1'
$ast = [System.Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors -join "`n") }
$definition = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Invoke-GitFetch' }, $true)
if (-not $definition) { throw 'Invoke-GitFetch was not found.' }
. ([scriptblock]::Create($definition.Extent.Text))

function Assert-True {
  param([bool]$Condition, [string]$Message)
  if (-not $Condition) { throw $Message }
}
function Reset-FakeGit {
  param([object[]]$Results)
  $script:results = $Results
  $script:calls = [System.Collections.Generic.List[object]]::new()
  $script:sleeps = @()
}
function git {
  $index = $script:calls.Count
  $script:calls.Add(@($args))
  if ($index -ge $script:results.Count) { throw 'Unexpected extra Git invocation.' }
  $result = $script:results[$index]
  $global:LASTEXITCODE = $result.Code
  if ($result.Message) { Write-Error -Message $result.Message }
}
function Start-Sleep {
  param([int]$Seconds)
  $script:sleeps += $Seconds
}
function Invoke-ExpectedFailure {
  $failed = $false
  try { Invoke-GitFetch 'origin' } catch { $failed = $true }
  Assert-True $failed 'Fetch must fail closed instead of continuing deployment.'
}

Reset-FakeGit @(@{ Code = 0; Message = '' })
Invoke-GitFetch 'origin'
Assert-True ($script:calls.Count -eq 1) 'Success must not cause retries.'
Assert-True ($script:sleeps.Count -eq 0) 'Success must not sleep.'
Assert-True (($script:calls[0] -join ' ') -match 'fetch -- origin$') 'Remote must follow the option terminator.'
Write-Output 'PASS: successful fetch'

Reset-FakeGit @(@{ Code = 128; Message = 'schannel: failed to receive handshake, SSL/TLS connection failed' }, @{ Code = 0; Message = '' })
Invoke-GitFetch 'origin'
Assert-True ($script:calls.Count -eq 2) 'TLS failure must be retried.'
Assert-True ($script:calls[0] -notcontains 'http.sslBackend=openssl') 'First attempt must respect the configured backend.'
Assert-True ($script:calls[1] -contains 'http.sslBackend=openssl') 'TLS retry must use OpenSSL.'
foreach ($call in $script:calls) {
  Assert-True ($call -contains 'http.sslVerify=true') 'Certificate verification must stay enabled.'
  Assert-True ($call -contains 'http.version=HTTP/1.1') 'Fetch must use HTTP/1.1.'
  Assert-True ($call -notcontains 'config') 'Retry must not change persisted Git configuration.'
}
Assert-True ($ErrorActionPreference -eq 'Stop') 'Error preference must be restored after success.'
Write-Output 'PASS: TLS retry keeps verification and uses scoped OpenSSL'

Reset-FakeGit @(@{ Code = 128; Message = 'Failed to connect to github.com' }, @{ Code = 128; Message = 'Connection reset by peer' }, @{ Code = 128; Message = 'Could not resolve host: github.com' })
Invoke-ExpectedFailure
Assert-True ($script:calls.Count -eq 3) 'Network errors must stop after exactly three attempts.'
Assert-True (($script:sleeps -join ',') -eq '2,4') 'Retry backoff must be bounded.'
Assert-True ($script:calls[1] -notcontains 'http.sslBackend=openssl') 'Non-TLS errors must preserve the configured backend.'
Assert-True ($ErrorActionPreference -eq 'Stop') 'Error preference must be restored after failure.'
Write-Output 'PASS: persistent network failure stops after three attempts'

Reset-FakeGit @(@{ Code = 128; Message = 'fatal: Authentication failed' })
Invoke-ExpectedFailure
Assert-True ($script:calls.Count -eq 1) 'Authentication errors must not be retried.'
Assert-True ($script:sleeps.Count -eq 0) 'Authentication errors must not sleep.'
Write-Output 'PASS: authentication failure stops immediately'
Write-Output 'Git fetch regression tests: 4 passed'
