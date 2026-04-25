$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$statePath = Join-Path $repoRoot ".run\dev\pids.json"
$managedPorts = @(5173, 8081)

function Get-DescendantProcessIds {
    param(
        [Parameter(Mandatory = $true)]
        [int]$RootPid,
        [Parameter(Mandatory = $true)]
        [object[]]$ProcessTable
    )

    $descendants = New-Object System.Collections.Generic.List[int]
    $queue = New-Object System.Collections.Generic.Queue[int]
    $queue.Enqueue($RootPid)

    while ($queue.Count -gt 0) {
        $current = $queue.Dequeue()
        $children = $ProcessTable | Where-Object { $_.ParentProcessId -eq $current }
        foreach ($child in $children) {
            if (-not $descendants.Contains([int]$child.ProcessId)) {
                $descendants.Add([int]$child.ProcessId)
                $queue.Enqueue([int]$child.ProcessId)
            }
        }
    }

    return $descendants.ToArray()
}

function Get-AncestorProcessIds {
    param(
        [Parameter(Mandatory = $true)]
        [int]$ProcessId,
        [Parameter(Mandatory = $true)]
        [object[]]$ProcessTable
    )

    $ancestors = New-Object System.Collections.Generic.List[int]
    $currentPid = $ProcessId

    while ($true) {
        $process = $ProcessTable | Where-Object { $_.ProcessId -eq $currentPid } | Select-Object -First 1
        if ($null -eq $process) {
            break
        }

        $parentPid = [int]$process.ParentProcessId
        if ($parentPid -le 0) {
            break
        }

        if ($ancestors.Contains($parentPid)) {
            break
        }

        $ancestors.Add($parentPid)
        $currentPid = $parentPid
    }

    return $ancestors.ToArray()
}

function Add-UniquePid {
    param(
        [Parameter(Mandatory = $true)]
        $Target,
        [Parameter(Mandatory = $true)]
        [int]$ProcessId
    )

    if ($null -eq $Target) {
        return
    }

    if (-not $Target.Contains($ProcessId)) {
        $Target.Add($ProcessId)
    }
}

function Get-ServicePidsFromState {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$State,
        [Parameter(Mandatory = $true)]
        [object[]]$ProcessTable
    )

    $allPids = New-Object System.Collections.Generic.List[int]

    foreach ($service in $State.services) {
        $rootPid = [int]$service.pid
            Add-UniquePid -Target $allPids -ProcessId $rootPid

        foreach ($procId in @(Get-DescendantProcessIds -RootPid $rootPid -ProcessTable $ProcessTable)) {
            Add-UniquePid -Target $allPids -ProcessId ([int]$procId)
        }
    }

    return $allPids.ToArray()
}

function Get-ServicePidsFromPortFallback {
    param(
        [Parameter(Mandatory = $true)]
        [int[]]$Ports,
        [Parameter(Mandatory = $true)]
        [object[]]$ProcessTable
    )

    $targetPids = New-Object System.Collections.Generic.List[int]
    $listeners = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -in $Ports }

    foreach ($listener in $listeners) {
        $listenerPid = [int]$listener.OwningProcess
        $candidatePids = New-Object System.Collections.Generic.List[int]
        $listenerPort = [int]$listener.LocalPort

        Add-UniquePid -Target $candidatePids -ProcessId $listenerPid

        foreach ($procId in @(Get-AncestorProcessIds -ProcessId $listenerPid -ProcessTable $ProcessTable)) {
            Add-UniquePid -Target $candidatePids -ProcessId ([int]$procId)
        }

        foreach ($procId in @(Get-DescendantProcessIds -RootPid $listenerPid -ProcessTable $ProcessTable)) {
            Add-UniquePid -Target $candidatePids -ProcessId ([int]$procId)
        }

        foreach ($procId in $candidatePids) {
            Add-UniquePid -Target $targetPids -ProcessId ([int]$procId)
        }

        Write-Host "Found leftover listener on port $listenerPort, stopping process chain [$($candidatePids -join ', ')]."
    }

    return $targetPids.ToArray()
}

function Stop-PidSet {
    param(
        [Parameter(Mandatory = $true)]
        [int[]]$Pids
    )

    foreach ($procId in ($Pids | Sort-Object -Descending -Unique)) {
        $process = Get-Process -Id $procId -ErrorAction SilentlyContinue
        if ($null -eq $process) {
            continue
        }

        Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
    }
}

$processTable = @(Get-CimInstance Win32_Process)

if (Test-Path $statePath) {
    $state = Get-Content $statePath -Raw | ConvertFrom-Json
    $servicePids = @(Get-ServicePidsFromState -State $state -ProcessTable $processTable)

    Stop-PidSet -Pids $servicePids

    foreach ($service in $state.services) {
        $rootPid = [int]$service.pid
        $descendantPids = @(Get-DescendantProcessIds -RootPid $rootPid -ProcessTable $processTable)
        $stoppedPidsText = (@($rootPid) + $descendantPids | Sort-Object -Unique) -join ", "
        Write-Host "$($service.name): stopped process tree [$stoppedPidsText]"
    }

    Remove-Item -LiteralPath $statePath -Force
    Write-Host "Local dev stack has been stopped."
    exit 0
}

$fallbackPids = @(Get-ServicePidsFromPortFallback -Ports $managedPorts -ProcessTable $processTable)
if ($fallbackPids.Count -eq 0) {
    Write-Host "No running local dev state was found."
    exit 0
}

Stop-PidSet -Pids $fallbackPids
$fallbackText = ($fallbackPids | Sort-Object -Unique) -join ", "
Write-Host "Recovered and stopped leftover local dev processes [$fallbackText]."
