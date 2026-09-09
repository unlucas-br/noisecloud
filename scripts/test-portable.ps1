param([Parameter(Mandatory = $true)][string]$Executable)
$ErrorActionPreference = 'Stop'
$source = (Resolve-Path -LiteralPath $Executable).Path
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('ncc-portable-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot | Out-Null
# Only the single downloaded executable is copied. No DLLs or FFmpeg sibling.
$exe = Join-Path $testRoot 'ncc.exe'
Copy-Item -LiteralPath $source -Destination $exe
$inputFile = Join-Path $testRoot 'input.bin'
$video = Join-Path $testRoot 'video.mp4'
$outputFile = Join-Path $testRoot 'recovered.bin'
$data = [byte[]]::new(1100)
[Security.Cryptography.RandomNumberGenerator]::Fill($data)
[IO.File]::WriteAllBytes($inputFile, $data)

function Invoke-Portable([string[]]$Answers) {
    $info = [Diagnostics.ProcessStartInfo]::new($exe)
    $info.WorkingDirectory = $testRoot
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardInput = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.Environment['PATH'] = ''
    $info.Environment['LOCALAPPDATA'] = Join-Path $testRoot 'cache'
    $info.Environment['USERPROFILE'] = $testRoot
    $process = [Diagnostics.Process]::Start($info)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        $process.StandardInput.Write(($Answers -join "`n"))
        $process.StandardInput.Close()
        if (-not $process.WaitForExit(120000)) {
            $process.Kill($true)
            throw 'Portable test timed out'
        }
        $log = $stdout.GetAwaiter().GetResult()
        $errors = $stderr.GetAwaiter().GetResult()
        if ($process.ExitCode -ne 0 -or $log -match 'Erro:') {
            throw "Portable test failed: $log $errors"
        }
    } finally {
        $process.Dispose()
    }
}

try {
    Invoke-Portable @('1', $inputFile, $video, '', '5', '')
    Invoke-Portable @('3', $video, $outputFile, '', '5', '')
    if ((Get-FileHash $inputFile).Hash -ne (Get-FileHash $outputFile).Hash) {
        throw 'Portable round trip mismatch'
    }
    $cached = Get-ChildItem (Join-Path $testRoot 'cache/NoiseCloud/ffmpeg') -Filter ffmpeg.exe -Recurse
    if (@($cached).Count -ne 1) { throw 'Embedded FFmpeg was not extracted into the isolated cache' }
    Write-Host 'Single EXE encode/decode passed with empty PATH and fresh cache.'
} finally {
    # The target is the unique test directory created above, never user data.
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and [IO.Path]::GetFileName($resolved).StartsWith('ncc-portable-')) {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
