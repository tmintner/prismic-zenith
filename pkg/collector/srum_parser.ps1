# SRUM Parser using .NET ESE Interop
param(
    [string]$DbPath = "C:\Windows\System32\sru\SRUDB.dat"
)

Add-Type -AssemblyName Microsoft.Isam.Esent.Interop
using namespace Microsoft.Isam.Esent.Interop

$destPath = Join-Path $env:TEMP "SRUDB_copy.dat"

# 1. Copy locked file
try {
    $srcStream = [System.IO.File]::Open($DbPath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
    $destStream = [System.IO.File]::Create($destPath)
    $srcStream.CopyTo($destStream)
    $srcStream.Close()
    $destStream.Close()
} catch {
    Write-Error "Failed to copy SRUDB.dat: $_"
    exit 1
}

# 2. Open ESE Database
$instance = New-Object Microsoft.Isam.Esent.Interop.Instance("SRUMParser")
$instance.Parameters.CircularLog = $true
$instance.Init()

$session = New-Object Microsoft.Isam.Esent.Interop.Session($instance)
$dbid = [Microsoft.Isam.Esent.Interop.Api]::JetOpenDatabase($session, $destPath, $null, [Microsoft.Isam.Esent.Interop.OpenDatabaseGrbit]::ReadOnly)

# 3. Helper to get SrumIdMap
$idMap = @{}
$mapTable = New-Object Microsoft.Isam.Esent.Interop.Table($session, $dbid, "SrumIdMapTable", [Microsoft.Isam.Esent.Interop.OpenTableGrbit]::ReadOnly)
while ([Microsoft.Isam.Esent.Interop.Api]::TryMoveFirst($session, $mapTable) -or [Microsoft.Isam.Esent.Interop.Api]::TryMoveNext($session, $mapTable)) {
    $id = [Microsoft.Isam.Esent.Interop.Api]::RetrieveColumnAsInt32($session, $mapTable, [Microsoft.Isam.Esent.Interop.Api]::GetTableColumnid($session, $mapTable, "Id"))
    $value = [Microsoft.Isam.Esent.Interop.Api]::RetrieveColumnAsString($session, $mapTable, [Microsoft.Isam.Esent.Interop.Api]::GetTableColumnid($session, $mapTable, "Value"))
    $idMap[$id] = $value
}
$mapTable.Close()

# 4. Read Application Resource Usage table
$usageTableGuid = "{D10CA2FE-6FCF-4F6D-848E-B2E99266FA89}"
$results = @()
try {
    $usageTable = New-Object Microsoft.Isam.Esent.Interop.Table($session, $dbid, $usageTableGuid, [Microsoft.Isam.Esent.Interop.OpenTableGrbit]::ReadOnly)
    
    # Move to last 100 records for brevity/performance or filter by time
    # For this implementation, we'll just take the most recent ones
    if ([Microsoft.Isam.Esent.Interop.Api]::TryMoveLast($session, $usageTable)) {
        $count = 0
        do {
            $appId = [Microsoft.Isam.Esent.Interop.Api]::RetrieveColumnAsInt32($session, $usageTable, [Microsoft.Isam.Esent.Interop.Api]::GetTableColumnid($session, $usageTable, "AppId"))
            $cycleTime = [Microsoft.Isam.Esent.Interop.Api]::RetrieveColumnAsInt64($session, $usageTable, [Microsoft.Isam.Esent.Interop.Api]::GetTableColumnid($session, $usageTable, "CycleTime"))
            $bytesRead = [Microsoft.Isam.Esent.Interop.Api]::RetrieveColumnAsInt64($session, $usageTable, [Microsoft.Isam.Esent.Interop.Api]::GetTableColumnid($session, $usageTable, "BytesRead"))
            $bytesWritten = [Microsoft.Isam.Esent.Interop.Api]::RetrieveColumnAsInt64($session, $usageTable, [Microsoft.Isam.Esent.Interop.Api]::GetTableColumnid($session, $usageTable, "BytesWritten"))
            
            $results += [PSCustomObject]@{
                AppName = $idMap[$appId]
                CycleTime = $cycleTime
                BytesRead = $bytesRead
                BytesWritten = $bytesWritten
            }
            $count++
        } while ($count -lt 50 -and [Microsoft.Isam.Esent.Interop.Api]::TryMovePrevious($session, $usageTable))
    }
    $usageTable.Close()
} catch {
    Write-Warning "Failed to read Application Resource Usage table: $_"
}

# 5. Cleanup
$session.Dispose()
$instance.Dispose()
Remove-Item $destPath -Force

# 6. Output JSON
$results | ConvertTo-Json
