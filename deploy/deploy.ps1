[CmdletBinding()]
param(
  [ValidateSet('Deploy', 'Preflight', 'Status', 'Logs')]
  [string]$Action = 'Deploy',
  [string]$Server,
  [string]$RemoteUser,
  [string]$IdentityFile,
  [string]$InstallDir,
  [string]$ProjectName,
  [string]$PublicUrl
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Invoke-Checked {
  param([string]$Command, [string[]]$Arguments)
  & $Command @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw ("{0} failed with exit code {1}." -f $Command, $LASTEXITCODE)
  }
}

function Invoke-GitFetch {
  param([string]$Remote)
  $useOpenSsl = $false
  for ($attempt = 1; $attempt -le 3; $attempt++) {
    $gitArgs = @('-c', 'http.version=HTTP/1.1', '-c', 'http.sslVerify=true')
    if ($useOpenSsl) { $gitArgs += @('-c', 'http.sslBackend=openssl') }
    $gitArgs += @('fetch', '--', $Remote)
    # PS5.1 turns native stderr into ErrorRecords; inspect Git's exit code explicitly.
    $savedPreference = $ErrorActionPreference
    try {
      $ErrorActionPreference = 'Continue'
      $fetchOutput = @(& git @gitArgs 2>&1)
      $fetchExitCode = $LASTEXITCODE
    }
    finally {
      $ErrorActionPreference = $savedPreference
    }
    foreach ($line in $fetchOutput) { Write-Host ([string]$line) }
    if ($fetchExitCode -eq 0) { return }
    $diagnostic = ($fetchOutput | ForEach-Object { [string]$_ }) -join "`n"
    $tlsFailure = $diagnostic -match '(?i)schannel|handshake|SSL/TLS|SSL_connect|TLS connect'
    $networkFailure = $tlsFailure -or $diagnostic -match '(?i)could not resolve|failed to connect|connection.*(reset|timed out|closed)|recv failure|send failure|operation timed out|unexpected disconnect|error:\s*(429|500|502|503|504)'
    if (-not $networkFailure -or $attempt -eq 3) {
      throw "Git fetch failed with exit code $fetchExitCode after $attempt attempt(s). Deployment stopped before upload; check network, proxy, credentials and remote URL."
    }
    if ($tlsFailure -and -not $useOpenSsl) {
      $useOpenSsl = $true
      Write-Host 'TLS handshake failed; retrying with OpenSSL and HTTP/1.1. Certificate verification remains enabled.'
    }
    Write-Host ("Git network check failed; retrying ({0}/3)..." -f ($attempt + 1))
    Start-Sleep -Seconds (2 * $attempt)
  }
}

function Get-RequiredConfigValue {
  param([object]$Config, [string]$Name, [string]$Override)
  if (-not [string]::IsNullOrWhiteSpace($Override)) { return $Override }
  $property = $Config.PSObject.Properties[$Name]
  if ($null -eq $property -or [string]::IsNullOrWhiteSpace([string]$property.Value)) {
    throw ("Missing required deployment setting '{0}'. Set it in .local/deploy/server.json or pass -{0}." -f $Name)
  }
  return [string]$property.Value
}

function Assert-SafeRemoteValue {
  param([string]$Name, [string]$Value, [string]$Pattern)
  if ($Value -notmatch $Pattern) {
    throw ("Deployment setting '{0}' contains unsupported characters." -f $Name)
  }
}

function Quote-RemoteArgument {
  param([string]$Value)
  # All CLI values are validated against strict whitelists before reaching this function.
  return "'" + $Value + "'"
}

function Invoke-RemoteCommand {
  param([string]$CommandLine)
  Invoke-Checked 'ssh' ($sshOptions + @($target, $CommandLine))
}

function Invoke-RemoteReadOnly {
  param([string]$RemoteAction)
  $scriptPath = Join-Path $repoRoot 'deploy\remote-deploy.py'
  if (-not (Test-Path -LiteralPath $scriptPath -PathType Leaf)) {
    throw "Cannot find remote CLI: $scriptPath"
  }
  $scriptText = [System.IO.File]::ReadAllText($scriptPath, [System.Text.Encoding]::UTF8)
  $payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($scriptText))
  $remoteArgs = @(
    $RemoteAction,
    '--install-dir', $resolvedInstallDir,
    '--project-name', $resolvedProjectName,
    '--public-url', $resolvedPublicUrl
  ) | ForEach-Object { Quote-RemoteArgument ([string]$_) }
  # PS5.1 may wrap piped native stdin in a UTF-8 BOM and CRLF; -i skips those non-alphabet bytes.
  # Without pipefail a failed decode feeds python3 an empty script and still reports success.
  $remoteCommand = "set -o pipefail; base64 -d -i | sudo -n python3 - $($remoteArgs -join ' ')"
  $payload | & ssh @sshOptions $target $remoteCommand
  if ($LASTEXITCODE -ne 0) {
    throw ("ssh failed with exit code {0}." -f $LASTEXITCODE)
  }
}

function Get-RemoteSha256 {
  param([string]$RemotePath)
  $quotedPath = Quote-RemoteArgument $RemotePath
  $output = @(& ssh @sshOptions $target "sha256sum -- $quotedPath" 2>&1)
  $exitCode = $LASTEXITCODE
  if ($exitCode -ne 0) { throw ("Remote SHA-256 check failed for {0}. {1}" -f $RemotePath, ($output -join "`n")) }
  $joined = ($output -join "`n").Trim()
  if ($joined -notmatch '^([a-fA-F0-9]{64})\s') { throw "Unexpected remote sha256sum output for $RemotePath." }
  return $Matches[1].ToLowerInvariant()
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$configPath = Join-Path $repoRoot '.local\deploy\server.json'
if (Test-Path -LiteralPath $configPath -PathType Leaf) {
  try {
    $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json
  }
  catch {
    throw "Could not parse deployment config '$configPath': $($_.Exception.Message)"
  }
} else {
  $config = [pscustomobject]@{}
}

$resolvedServer = Get-RequiredConfigValue $config 'Server' $Server
$resolvedRemoteUser = Get-RequiredConfigValue $config 'RemoteUser' $RemoteUser
$resolvedIdentityFile = Get-RequiredConfigValue $config 'IdentityFile' $IdentityFile
$resolvedInstallDir = Get-RequiredConfigValue $config 'InstallDir' $InstallDir
$resolvedProjectName = Get-RequiredConfigValue $config 'ProjectName' $ProjectName
$resolvedPublicUrl = Get-RequiredConfigValue $config 'PublicUrl' $PublicUrl

Assert-SafeRemoteValue 'Server' $resolvedServer '^[A-Za-z0-9.-]+$'
Assert-SafeRemoteValue 'RemoteUser' $resolvedRemoteUser '^[A-Za-z_][A-Za-z0-9_-]*$'
Assert-SafeRemoteValue 'InstallDir' $resolvedInstallDir '^/[A-Za-z0-9._/-]+$'
Assert-SafeRemoteValue 'ProjectName' $resolvedProjectName '^[a-z0-9][a-z0-9_-]*$'
Assert-SafeRemoteValue 'PublicUrl' $resolvedPublicUrl '^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?/?$'
if ($resolvedInstallDir -match '/\.\.?(/|$)') { throw "InstallDir must not contain dot path components." }
$resolvedPublicUri = [Uri]$resolvedPublicUrl
if ($resolvedPublicUri.Port -lt 1 -or $resolvedPublicUri.Port -gt 65535) { throw 'PublicUrl port is out of range.' }

if (-not [System.IO.Path]::IsPathRooted($resolvedIdentityFile)) {
  $resolvedIdentityFile = Join-Path $repoRoot $resolvedIdentityFile
}
if (-not (Test-Path -LiteralPath $resolvedIdentityFile -PathType Leaf)) {
  throw "SSH identity file does not exist: $resolvedIdentityFile"
}
$resolvedIdentityFile = (Resolve-Path -LiteralPath $resolvedIdentityFile).Path
$target = "$resolvedRemoteUser@$resolvedServer"
$sshOptions = @(
  '-i', $resolvedIdentityFile,
  '-o', 'KexAlgorithms=ecdh-sha2-nistp256',
  '-o', 'Ciphers=aes128-gcm@openssh.com',
  '-o', 'IPQoS=none',
  '-o', 'BatchMode=yes',
  '-o', 'ConnectTimeout=15',
  '-o', 'ServerAliveInterval=15',
  '-o', 'StrictHostKeyChecking=yes'
)

switch ($Action) {
  'Preflight' { Invoke-RemoteReadOnly 'preflight'; exit 0 }
  'Status' { Invoke-RemoteReadOnly 'status'; exit 0 }
  'Logs' { Invoke-RemoteReadOnly 'logs'; exit 0 }
}

Push-Location $repoRoot
try {
  $workingTree = (& git status --porcelain --untracked-files=all) -join "`n"
  if ($LASTEXITCODE -ne 0) { throw 'Could not inspect Git working tree.' }
  if ($workingTree) { throw "Working tree is not clean, including untracked files. Commit or remove changes before Deploy:`n$workingTree" }

  $requiredPaths = @(((-join ([char[]]@(19968, 38190, 37096, 32626))) + '.bat'), 'deploy/deploy.ps1', 'deploy/remote-deploy.py')
  foreach ($path in $requiredPaths) {
    & git cat-file -e "HEAD:$path" 2>$null
    if ($LASTEXITCODE -ne 0) { throw "HEAD does not contain required deployment file '$path'. Commit the deployment files before Deploy." }
  }
  $commit = (& git rev-parse --verify HEAD).Trim()
  if ($LASTEXITCODE -ne 0 -or $commit -notmatch '^[0-9a-f]{40}$') { throw 'Could not read current Git commit.' }

  $upstream = (& git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>$null).Trim()
  if ($LASTEXITCODE -ne 0 -or -not $upstream -or $upstream -notmatch '^([^/]+)/(.+)$') {
    throw 'Current branch has no valid upstream tracking branch.'
  }
  $upstreamRemote = $Matches[1]
  Invoke-GitFetch $upstreamRemote
  $upstreamCommit = (& git rev-parse --verify '@{u}').Trim()
  if ($LASTEXITCODE -ne 0 -or $upstreamCommit -notmatch '^[0-9a-f]{40}$') { throw "Could not resolve fetched upstream '$upstream'." }
  if ($commit -ne $upstreamCommit) { throw "Current commit is not pushed to $upstream. Local: $commit; upstream: $upstreamCommit." }

  Write-Host 'Checking remote deployment and BuildKit before uploading source...'
  Invoke-RemoteReadOnly 'preflight'

  $guid = [Guid]::NewGuid().ToString('N')
  $cacheRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot '.local\cache\deploy'))
  $tempDir = [System.IO.Path]::GetFullPath((Join-Path $cacheRoot $guid))
  if (-not $tempDir.StartsWith($cacheRoot + [System.IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Refusing to write deployment archive outside the local deployment cache.'
  }
  New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

  $archive = Join-Path $tempDir "canvas-$commit-$guid.tar.gz"
  $remoteCli = Join-Path $tempDir 'remote-deploy.py'
  $remoteArchive = "/tmp/canvas-$commit-$guid.tar.gz"
  $remoteScript = "/tmp/canvas-deploy-$guid.py"

  Invoke-Checked 'git' @('archive', '--format=tar.gz', "--output=$archive", $commit)
  $remoteContent = (& git show "${commit}:deploy/remote-deploy.py") -join "`n"
  if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($remoteContent)) { throw 'Could not extract deploy/remote-deploy.py from the committed revision.' }
  $remoteContent = $remoteContent.Replace("`r`n", "`n").Replace("`r", "`n")
  [System.IO.File]::WriteAllText($remoteCli, $remoteContent.TrimEnd([char]10) + "`n", [System.Text.UTF8Encoding]::new($false))

  $archiveHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
  $scriptHash = (Get-FileHash -LiteralPath $remoteCli -Algorithm SHA256).Hash.ToLowerInvariant()
  Invoke-Checked 'scp' ($sshOptions + @($archive, "${target}:$remoteArchive"))
  Invoke-Checked 'scp' ($sshOptions + @($remoteCli, "${target}:$remoteScript"))
  if ((Get-RemoteSha256 $remoteArchive) -ne $archiveHash) { throw 'Uploaded source archive SHA-256 does not match.' }
  if ((Get-RemoteSha256 $remoteScript) -ne $scriptHash) { throw 'Uploaded remote CLI SHA-256 does not match.' }

  $deployArgs = @(
    'deploy', '--install-dir', $resolvedInstallDir,
    '--project-name', $resolvedProjectName,
    '--public-url', $resolvedPublicUrl,
    '--archive', $remoteArchive,
    '--sha256', $archiveHash,
    '--commit', $commit
  ) | ForEach-Object { Quote-RemoteArgument ([string]$_) }
  $remoteCommand = "sudo -n python3 $(Quote-RemoteArgument $remoteScript) $($deployArgs -join ' ')"
  Invoke-RemoteCommand $remoteCommand
  Write-Host ("Deploy completed for commit {0}. Local archive retained at: {1}" -f $commit, $archive)
}
finally {
  Pop-Location
}
exit 0
