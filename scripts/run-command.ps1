function Invoke-BoundedCommand {
    param([string]$Executable, [string[]]$Arguments, [int]$TimeoutSeconds = 60, [string]$WorkingDirectory = (Get-Location).Path)
    $command = Get-Command $Executable -CommandType Application -ErrorAction Stop
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo.FileName = $command.Source
    $process.StartInfo.WorkingDirectory = $WorkingDirectory
    $process.StartInfo.UseShellExecute = $false
    $process.StartInfo.CreateNoWindow = $true
    $process.StartInfo.RedirectStandardOutput = $true
    $process.StartInfo.RedirectStandardError = $true
    # Windows CRT quoting also works for .NET's Unix argument parser.
    $quoted = foreach ($argument in $Arguments) {
        '"' + [regex]::Replace([regex]::Replace($argument, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
    }
    $process.StartInfo.Arguments = $quoted -join ' '
    try {
        if (-not $process.Start()) { throw "Cannot start $Executable." }
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
            $process.Kill()
            throw "$Executable exceeded its $TimeoutSeconds second deadline. Check remote state before retrying a deployment."
        }
        if ($process.ExitCode -ne 0) { throw "$Executable failed with exit code $($process.ExitCode)." }
        $stdout.GetAwaiter().GetResult()
    } finally { $process.Dispose() }
}
