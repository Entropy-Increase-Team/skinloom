<#
  Runs SkinLoom in the background on Windows: starts with the computer (no login
  needed) and restarts after a crash. Uses Task Scheduler, which ships with Windows.

  In an administrator PowerShell, from the unpacked release folder:
    powershell -ExecutionPolicy Bypass -File .\service\install-task.ps1
  First start: the one-time setup link is in data\skinloom.log.
  Remove:     powershell -ExecutionPolicy Bypass -File .\service\install-task.ps1 -Uninstall

  The task runs as LOCAL SERVICE, which may only write to data\ and the frontend
  cache folder (granted below), not to the rest of the system.
#>
param(
  [string]$Dir = (Split-Path -Parent $PSScriptRoot),
  [string]$TaskName = 'SkinLoom',
  [switch]$Uninstall
)
$ErrorActionPreference = 'Stop'

if ($Uninstall) {
  Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
  Write-Host "Removed task $TaskName. Data in $Dir\data is kept."
  return
}

$exe = Join-Path $Dir 'skinloom.exe'
if (-not (Test-Path $exe)) { throw "skinloom.exe not found in $Dir (pass -Dir <release folder>)" }
$data = Join-Path $Dir 'data'
$cache = Join-Path $Dir 'web\apps\web\.next'
New-Item -ItemType Directory -Force -Path $data | Out-Null

# LOCAL SERVICE gets write access to the data folder and the frontend cache only.
foreach ($path in @($data, $cache)) {
  icacls $path /grant '*S-1-5-19:(OI)(CI)M' /T /Q | Out-Null # S-1-5-19 = LOCAL SERVICE
}

# Task Scheduler gives no console, so the log goes to a file via cmd's redirection.
$log = Join-Path $data 'skinloom.log'
$arguments = "/d /c `"`"$exe`" up --data `"$data`" >> `"$log`" 2>&1`""
$action = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument $arguments -WorkingDirectory $Dir
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RestartCount 999 `
  -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) `
  -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$principal = New-ScheduledTaskPrincipal -UserId 'NT AUTHORITY\LOCAL SERVICE' -LogonType ServiceAccount

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings `
  -Principal $principal -Description 'SkinLoom skin site' -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName
Write-Host "Installed and started task $TaskName. Open http://localhost:17546 ; log: $log"
