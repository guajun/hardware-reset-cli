[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Port,
    [ValidatePattern('^PA[0-7]$')][string]$Pin = 'PA0'
)

# This test drives the selected GPIO low, then releases it. It uses the actual
# CDC byte stream, independently of the Go client's protocol implementation.
$ErrorActionPreference = 'Stop'
$script:requestId = Get-Random -Minimum 1 -Maximum 1000000000
$serial = New-Object System.IO.Ports.SerialPort $Port,115200,None,8,One
$serial.ReadTimeout = 2000
$serial.WriteTimeout = 2000
$serial.NewLine = "`n"
$serial.DtrEnable = $true

function Send-Request([string]$Body) {
    $script:requestId++
    $serial.WriteLine("V1 $script:requestId $Body")
    return $script:requestId
}
function Read-Expected([string]$Expected) {
    $actual = $serial.ReadLine().TrimEnd("`r")
    if ($actual -ne $Expected) { throw "Expected '$Expected'; received '$actual'" }
}
function Hello {
    $serial.Write("`n")
    $id = Send-Request 'HELLO 1'
    Read-Expected "V1 $id OK HELLO HWRESET 1"
}
function Released {
    $id = Send-Request "STATUS $Pin"
    Read-Expected "V1 $id OK STATUS $Pin RELEASED 0"
}

try {
    $serial.Open()
    Hello
    Released

    $id = Send-Request "ASSERT $Pin 2000"
    Read-Expected "V1 $id OK ASSERT $Pin ASSERTED 2000"
    $serial.WriteLine("V1 $id ASSERT $Pin 2000")
    Read-Expected "V1 $id OK ASSERT $Pin ASSERTED 2000"
    $id = Send-Request "STATUS $Pin"
    $state = $serial.ReadLine().TrimEnd("`r")
    if ($state -notmatch "^V1 $id OK STATUS $Pin ASSERTED [1-9][0-9]*$") { throw "Invalid asserted status: $state" }
    $id = Send-Request "RESET $Pin 100"
    Read-Expected "V1 $id ERR BUSY"
    $id = Send-Request "RELEASE $Pin"
    Read-Expected "V1 $id OK RELEASE $Pin RELEASED"
    Released
    Write-Output 'PASS: assertion, duplicate response, status, busy and release'

    $pending = Send-Request "RESET $Pin 10000"
    $id = Send-Request "RELEASE $Pin"
    Read-Expected "V1 $pending ERR CANCELLED"
    Read-Expected "V1 $id OK RELEASE $Pin RELEASED"
    Released
    Write-Output 'PASS: RESET cancellation and response ordering'

    $id = Send-Request "ASSERT $Pin 20"
    Read-Expected "V1 $id OK ASSERT $Pin ASSERTED 20"
    Start-Sleep -Milliseconds 40
    Released
    $id = Send-Request 'RESET PA8 100'
    Read-Expected "V1 $id ERR INVALID_PIN"
    $serial.WriteLine(('X' * 260))
    Read-Expected 'V1 0 ERR LINE_TOO_LONG'
    Released
    Write-Output 'PASS: lease expiry, invalid pin and overlong-frame recovery'

    $id = Send-Request "ASSERT $Pin 10000"
    Read-Expected "V1 $id OK ASSERT $Pin ASSERTED 10000"
    $serial.DtrEnable = $false
    Start-Sleep -Milliseconds 50
    $serial.DtrEnable = $true
    Hello
    Released
    Write-Output 'PASS: DTR falling edge releases output and resets handshake'
} finally {
    if ($serial.IsOpen) {
        try { $null = Send-Request "RELEASE $Pin" } catch { }
        $serial.DtrEnable = $false
        $serial.Close()
    }
    $serial.Dispose()
}
