BeforeAll {
    . "$PSScriptRoot/test.ps1"
}

Describe "test.ps1" {
    It "NoSwitch_SelectsUnit" {
        Get-SelectedSuites -Switches @{} | Should -Be @("Unit")
    }

    It "All_ExpandsToSevenSuites" {
        Get-SelectedSuites -Switches @{ All = $true } | Should -Be @("Lint", "Unit", "Race", "Integration", "E2E", "Cover", "Fuzz")
    }

    It "All_UsesTenSecondFuzz" {
        Get-EffectiveFuzzTime -Switches @{ All = $true } -Requested "30s" | Should -Be "10s"
    }

    It "AllWithExplicitFuzzTime_KeepsRequestedTime" {
        Get-EffectiveFuzzTime -Switches @{ All = $true; FuzzTime = "5s" } -Requested "5s" | Should -Be "5s"
    }

    It "ExplicitSwitches_KeepDeclaredOrder" {
        Get-SelectedSuites -Switches @{ Unit = $true; Lint = $true } | Should -Be @("Lint", "Unit")
    }

    It "MissingFfmpeg_FailsIntegrationWithInstallHint" {
        Mock Get-Command { $null } -ParameterFilter { $Name -eq "ffmpeg" }
        Mock Get-Command { [pscustomobject]@{ Name = $Name } } -ParameterFilter { $Name -ne "ffmpeg" }
        Mock Write-Host {}

        Test-SuitePrerequisites -Suite "Integration" | Should -BeFalse

        Should -Invoke Write-Host -ParameterFilter { $Object -like "*winget install -e --id Gyan.FFmpeg*" }
    }

    It "PresentPrerequisites_Pass" {
        Mock Get-Command { [pscustomobject]@{ Name = $Name } }

        Test-SuitePrerequisites -Suite "Integration" | Should -BeTrue
    }

    It "FailedSuite_ReturnsExitCodeOne" {
        Mock Test-SuitePrerequisites { $true }
        Mock Invoke-Suite { $false }
        Mock Out-Host {}

        Invoke-SelectedSuites -Suites @("Unit") -Time "1s" | Should -Be 1
    }

    It "PassingSuites_ReturnExitCodeZero" {
        Mock Test-SuitePrerequisites { $true }
        Mock Invoke-Suite { $true }
        Mock Out-Host {}

        Invoke-SelectedSuites -Suites @("Unit", "Lint") -Time "1s" | Should -Be 0
    }

    It "BranchCoverage_ParsesLastRatio" {
        Get-BranchCoverage -Output @("Condition coverage: 9/10 (90.0%)") | Should -Be 90
    }

    It "BranchCoverage_WithoutRatio_IsZero" {
        Get-BranchCoverage -Output @("no output") | Should -Be 0
    }
}
