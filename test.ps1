param(
    [switch]$Unit,
    [switch]$Race,
    [switch]$Integration,
    [switch]$E2E,
    [switch]$Eval,
    [switch]$Live,
    [switch]$Fuzz,
    [string]$FuzzTime = "30s",
    [switch]$Cover,
    [switch]$Lint,
    [switch]$All
)

$DomainBranchCoverageFloor = 90
$ServicesBranchCoverageFloor = 80
$AllFuzzTime = "10s"
$GobcoModule = "github.com/rillig/gobco@v1.3.4"
$GolangciLintModule = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2"
$GovulncheckModule = "golang.org/x/vuln/cmd/govulncheck@v1.8.0"
$SuiteOrder = @("Lint", "Unit", "Race", "Integration", "E2E", "Eval", "Live", "Cover", "Fuzz")
$AllSuites = @("Lint", "Unit", "Race", "Integration", "E2E", "Cover", "Fuzz")

function Get-SelectedSuites {
    param([hashtable]$Switches)
    if ($Switches["All"]) {
        return $AllSuites
    }
    $selected = @($SuiteOrder | Where-Object { $Switches[$_] })
    if ($selected.Count -eq 0) {
        return @("Unit")
    }
    return $selected
}

function Get-EffectiveFuzzTime {
    param([hashtable]$Switches, [string]$Requested)
    if ($Switches["All"] -and -not $Switches.ContainsKey("FuzzTime")) {
        return $AllFuzzTime
    }
    return $Requested
}

function Get-SuitePrerequisites {
    param([string]$Suite)
    $prerequisites = @{
        "Race"        = @(@{ Name = "gcc"; Hint = "winget install -e --id MSYS2.MSYS2, then pacman -S mingw-w64-ucrt-x86_64-gcc" })
        "Integration" = @(
            @{ Name = "ffmpeg"; Hint = "winget install -e --id Gyan.FFmpeg" },
            @{ Name = "ffprobe"; Hint = "winget install -e --id Gyan.FFmpeg" })
        "E2E"         = @(
            @{ Name = "ffmpeg"; Hint = "winget install -e --id Gyan.FFmpeg" },
            @{ Name = "ffprobe"; Hint = "winget install -e --id Gyan.FFmpeg" })
        "Lint"        = @(
            @{ Name = "golangci-lint"; Hint = "go install $GolangciLintModule" },
            @{ Name = "govulncheck"; Hint = "go install $GovulncheckModule" })
    }
    $common = @(@{ Name = "go"; Hint = "winget install -e --id GoLang.Go" })
    return $common + @($prerequisites[$Suite] | Where-Object { $_ })
}

function Test-SuitePrerequisites {
    param([string]$Suite)
    foreach ($prerequisite in Get-SuitePrerequisites -Suite $Suite) {
        if (-not (Get-Command $prerequisite.Name -ErrorAction SilentlyContinue)) {
            Write-Host "$($prerequisite.Name) is missing for -$Suite. Install: $($prerequisite.Hint)"
            return $false
        }
    }
    return $true
}

function Invoke-Native {
    param([string]$Command, [string[]]$Arguments)
    & $Command @Arguments | Out-Host
    return $LASTEXITCODE -eq 0
}

function Invoke-LintSuite {
    $unformatted = & gofmt -l .
    if ($unformatted) {
        Write-Host "gofmt: unformatted files:`n$unformatted"
        return $false
    }
    if (-not (Invoke-Native "go" @("vet", "./..."))) { return $false }
    if (-not (Invoke-Native "golangci-lint" @("run", "./..."))) { return $false }
    return Invoke-Native "govulncheck" @("./...")
}

function Invoke-FuzzSuite {
    param([string]$Time)
    $packages = & go list ./...
    foreach ($package in $packages) {
        $targets = & go test -list '^Fuzz' $package | Where-Object { $_ -like "Fuzz*" }
        foreach ($target in $targets) {
            if (-not (Invoke-Native "go" @("test", "-run", '^$', "-fuzz", "^$target`$", "-fuzztime", $Time, $package))) { return $false }
        }
    }
    return $true
}

function Get-BranchCoverage {
    param([string[]]$Output)
    $line = $Output | Where-Object { $_ -match '(\d+)/(\d+)' } | Select-Object -Last 1
    if (-not $line -or -not ($line -match '(\d+)/(\d+)') -or [int]$Matches[2] -eq 0) {
        return 0
    }
    return [math]::Floor(100 * [int]$Matches[1] / [int]$Matches[2])
}

function Test-BranchCoverage {
    param([string]$Package, [int]$Floor)
    $output = & go run $GobcoModule "./$Package" 2>&1 | ForEach-Object { "$_" }
    $coverage = Get-BranchCoverage -Output $output
    Write-Host "branch coverage $Package = $coverage % (floor $Floor %)"
    return $coverage -ge $Floor
}

function Invoke-CoverSuite {
    if (-not (Invoke-Native "go" @("test", "./...", "-count=1", "-coverprofile=coverage.out"))) { return $false }
    if (-not (Test-BranchCoverage -Package "domain" -Floor $DomainBranchCoverageFloor)) { return $false }
    return Test-BranchCoverage -Package "services" -Floor $ServicesBranchCoverageFloor
}

function Invoke-Suite {
    param([string]$Suite, [string]$Time)
    switch ($Suite) {
        "Unit" { return Invoke-Native "go" @("test", "./...", "-shuffle=on", "-count=1") }
        "Race" { return Invoke-Native "go" @("test", "./...", "-race", "-count=1") }
        "Integration" { return Invoke-Native "go" @("test", "-tags=integration", "./...", "-count=1") }
        "E2E" { return Invoke-Native "go" @("test", "-tags=e2e", "./cmd/music-ops/...", "-count=1") }
        "Eval" { return Invoke-Native "go" @("test", "-tags=eval", "./infrastructure/...", "-count=1", "-timeout=0") }
        "Live" { return Invoke-Native "go" @("test", "-tags=live", "./cmd/music-ops/...", "-count=1") }
        "Fuzz" { return Invoke-FuzzSuite -Time $Time }
        "Cover" { return Invoke-CoverSuite }
        "Lint" { return Invoke-LintSuite }
    }
    return $false
}

function Invoke-SelectedSuites {
    param([string[]]$Suites, [string]$Time)
    $env:MUSIC_OPS_STRICT = "1"
    $results = foreach ($suite in $Suites) {
        $watch = [System.Diagnostics.Stopwatch]::StartNew()
        $passed = (Test-SuitePrerequisites -Suite $suite) -and (Invoke-Suite -Suite $suite -Time $Time)
        [pscustomobject]@{ Suite = $suite; Result = $(if ($passed) { "PASS" } else { "FAIL" }); Duration = $watch.Elapsed.ToString("mm\:ss") }
    }
    $results | Format-Table -AutoSize | Out-Host
    if ($results | Where-Object { $_.Result -eq "FAIL" }) {
        return 1
    }
    return 0
}

if ($MyInvocation.InvocationName -ne ".") {
    $suites = Get-SelectedSuites -Switches $PSBoundParameters
    $time = Get-EffectiveFuzzTime -Switches $PSBoundParameters -Requested $FuzzTime
    exit (Invoke-SelectedSuites -Suites $suites -Time $time)
}
