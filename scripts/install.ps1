# Installs the latest pigo release for Windows into $env:LOCALAPPDATA\Programs\pigo.
#
#   irm https://raw.githubusercontent.com/ekmwest/pigo/main/scripts/install.ps1 | iex

# iex runs this in the caller's scope; the script block keeps our variables and
# preference changes out of the user's session.
& {
    $ErrorActionPreference = 'Stop'
    # The Invoke-WebRequest progress bar makes downloads many times slower in
    # Windows PowerShell 5.1.
    $ProgressPreference = 'SilentlyContinue'

    $baseUrl = 'https://github.com/ekmwest/pigo/releases/latest/download'
    $installDir = Join-Path $env:LOCALAPPDATA 'Programs\pigo'

    function Get-PigoArch {
        # OSArchitecture reports the real CPU even when PowerShell itself runs
        # as x64 under emulation on ARM. Older .NET Framework lacks the type,
        # so fall back to the environment variables.
        $arch = $null
        try {
            $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        } catch {
            $arch = $env:PROCESSOR_ARCHITEW6432
            if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
        }
        switch ($arch) {
            { $_ -in 'X64', 'AMD64' } { return 'amd64' }
            'Arm64' { return 'arm64' }
            default { throw "unsupported CPU architecture: $arch" }
        }
    }

    try {
        # $IsWindows only exists in PowerShell 6+, where it can be false.
        if ($PSVersionTable.PSVersion.Major -ge 6 -and -not $IsWindows) {
            throw 'install.ps1 is for Windows; on macOS and linux, use install.sh'
        }

        # Windows PowerShell 5.1 may default to TLS 1.0, which GitHub rejects.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

        $archive = "pigo_windows_$(Get-PigoArch).zip"
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ("pigo-install-" + [guid]::NewGuid())
        New-Item -ItemType Directory -Path $tmp | Out-Null

        try {
            Write-Host "Downloading $archive"
            foreach ($name in $archive, 'checksums.txt') {
                try {
                    Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/$name" -OutFile (Join-Path $tmp $name)
                } catch {
                    throw "download failed: $baseUrl/$name ($($_.Exception.Message))"
                }
            }

            $expected = $null
            foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
                $fields = $line -split '\s+'
                if ($fields.Count -ge 2 -and $fields[1] -eq $archive) {
                    $expected = $fields[0]
                    break
                }
            }
            if (-not $expected) { throw "no checksum for $archive in checksums.txt" }
            $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $archive)).Hash
            # -ne is case-insensitive, matching sha256sum's lowercase hex against Get-FileHash's uppercase.
            if ($actual -ne $expected) {
                throw "checksum mismatch for $archive (expected $expected, got $($actual.ToLower()))"
            }

            Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force

            New-Item -ItemType Directory -Path $installDir -Force | Out-Null
            $exe = Join-Path $installDir 'pigo.exe'
            try {
                Copy-Item -Path (Join-Path $tmp 'pigo.exe') -Destination $exe -Force
            } catch {
                throw "could not write $exe; if pigo is running, stop it and try again ($($_.Exception.Message))"
            }
        } finally {
            Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        }

        # Read the user PATH straight from the registry, unexpanded: going through
        # [Environment]::GetEnvironmentVariable would turn entries like
        # %USERPROFILE%\... into fixed paths when written back.
        $envKey = 'HKCU:\Environment'
        $userPath = (Get-Item $envKey).GetValue('Path', '', 'DoNotExpandEnvironmentNames')
        $entries = @($userPath -split ';' | Where-Object { $_ })
        # Compare expanded, without case or trailing backslash, so rerunning the
        # installer never adds a second copy.
        $onPath = $entries | Where-Object {
            [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') -eq $installDir.TrimEnd('\')
        }
        if (-not $onPath) {
            Set-ItemProperty -Path $envKey -Name 'Path' -Value (($entries + $installDir) -join ';') -Type ExpandString
            # A registry write alone doesn't notify Explorer, so new terminals
            # wouldn't see the change until the next sign-in. SetEnvironmentVariable
            # broadcasts that notification; a throwaway variable triggers it.
            [Environment]::SetEnvironmentVariable('PIGO_INSTALL', '1', 'User')
            [Environment]::SetEnvironmentVariable('PIGO_INSTALL', $null, 'User')
            Write-Host "Added $installDir to your user PATH"
        }
        # New terminals pick up the user PATH; this makes pigo work in the current one too.
        if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -eq $installDir.TrimEnd('\') })) {
            $env:Path = "$env:Path;$installDir"
        }

        Write-Host "Installed $(& $exe version) to $exe"
    } catch {
        # Never 'exit' here: under iex it would close the user's PowerShell window.
        Write-Host "pigo install: $($_.Exception.Message)" -ForegroundColor Red
    }
}
