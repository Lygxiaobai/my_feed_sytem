$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$runtimeDir = Join-Path $repoRoot ".run\dev"
$logsDir = Join-Path $runtimeDir "logs"
$statePath = Join-Path $runtimeDir "pids.json"

function Assert-Command {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$InstallHint
    )

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Missing command '$Name'. $InstallHint"
    }
}

function Assert-PortAvailable {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port,
        [Parameter(Mandatory = $true)]
        [string]$ServiceName
    )

    $listener = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -ne $listener) {
        throw "$ServiceName needs port $Port, but it is already used by PID $($listener.OwningProcess). Run .\stop-local.ps1 first or free the port."
    }
}

function Start-ManagedProcess {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$WorkingDirectory,
        [Parameter(Mandatory = $true)]
        [string]$Command
    )

    $stdoutPath = Join-Path $logsDir "$($Name).stdout.log"
    $stderrPath = Join-Path $logsDir "$($Name).stderr.log"

    $process = Start-Process `
        -FilePath "powershell.exe" `
        -ArgumentList @("-NoLogo", "-NoProfile", "-Command", $Command) `
        -WorkingDirectory $WorkingDirectory `
        -WindowStyle Hidden `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath `
        -PassThru

    Start-Sleep -Seconds 2
    $process.Refresh()

    if ($process.HasExited) {
        throw "$Name failed to start. Check $stdoutPath and $stderrPath."
    }

    return [ordered]@{
        name = $Name
        pid = $process.Id
        workdir = $WorkingDirectory
        stdout = $stdoutPath
        stderr = $stderrPath
    }
}

Assert-Command -Name "go" -InstallHint "Install Go 1.25+ and make sure it is available in PATH."
Assert-Command -Name "npm" -InstallHint "Install Node.js/npm and make sure it is available in PATH."

New-Item -ItemType Directory -Force -Path $logsDir | Out-Null

if (Test-Path $statePath) {
    throw "Existing dev state found at $statePath. Run .\stop-local.ps1 before starting again."
}

Assert-PortAvailable -Port 8081 -ServiceName "backend API"
Assert-PortAvailable -Port 5173 -ServiceName "frontend dev server"

$frontendNodeModules = Join-Path $repoRoot "frontend\node_modules"
if (-not (Test-Path $frontendNodeModules)) {
    Write-Host "frontend/node_modules was not found. Running npm install ..."
    Push-Location (Join-Path $repoRoot "frontend")
    try {
        & npm install
    } finally {
        Pop-Location
    }
}

$services = @()

try {
    $services += Start-ManagedProcess `
        -Name "api" `
        -WorkingDirectory (Join-Path $repoRoot "backend") `
        -Command "go run ./cmd"

    $services += Start-ManagedProcess `
        -Name "worker" `
        -WorkingDirectory (Join-Path $repoRoot "backend") `
        -Command "go run ./cmd/worker"

    $services += Start-ManagedProcess `
        -Name "frontend" `
        -WorkingDirectory (Join-Path $repoRoot "frontend") `
        -Command "npm run dev"

    [ordered]@{
        started_at = (Get-Date).ToString("o")
        services = $services
    } | ConvertTo-Json -Depth 4 | Set-Content -Path $statePath -Encoding UTF8
} catch {
    foreach ($service in $services) {
        Stop-Process -Id $service.pid -Force -ErrorAction SilentlyContinue
    }

    if (Test-Path $statePath) {
        Remove-Item -LiteralPath $statePath -Force
    }

    throw
}

Write-Host ""
Write-Host "Local dev stack is up:"
Write-Host "  Frontend: http://127.0.0.1:5173"
Write-Host "  Backend : http://127.0.0.1:8081"
Write-Host "  Config  : backend/configs/config.yaml"
Write-Host ""
Write-Host "Logs : $logsDir"
Write-Host "Stop : .\stop-local.ps1"
