#Requires -Version 5
#Requires -RunAsAdministrator
#Requires -Modules ScheduledTasks
<#
.SYNOPSIS
  Google Cloud Agent for SAP uninstall script.
.DESCRIPTION
  This powershell script is used to uninstall the Google Cloud Agent for SAP
  on the system and remove a Task Scheduler entry: google-cloud-sap-agent-monitor,
  .
#>
$ErrorActionPreference = 'Stop'
$INSTALL_DIR = 'C:\Program Files\Google\google-cloud-sap-agent'
$SVC_NAME = 'google-cloud-sap-agent'
$MONITOR_TASK = 'google-cloud-sap-agent-monitor'

try {
  # stop the service / tasks and remove them
  if ($(Get-ScheduledTask $MONITOR_TASK -ErrorAction Ignore).TaskName) {
    Disable-ScheduledTask $MONITOR_TASK
    Unregister-ScheduledTask -TaskName $MONITOR_TASK -Confirm:$false
  }
  if ($(Get-Service -Name $SVC_NAME -ErrorAction SilentlyContinue).Length -gt 0) {
    Stop-Service $SVC_NAME
    $service = Get-CimInstance -ClassName Win32_Service -Filter "Name='google-cloud-sap-agent'"
    $service.Dispose()
    # without the ampersand PowerShell will block removal of the service for some time.
    & sc.exe delete $SVC_NAME
  }

  # remove the agent directory
  if (Test-Path $INSTALL_DIR) {
    Remove-Item -Recurse -Force $INSTALL_DIR
  }
}
catch {
  break
}
