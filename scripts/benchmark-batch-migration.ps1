param(
    [string]$ConnectionString = $env:TARGET_DATABASE_URL,
    [string]$ApiBaseUrl = $env:DBMP_API_BASE_URL,
    [string]$DatabaseId = $env:DBMP_DATABASE_ID,
    [string]$MigrationId = $env:DBMP_MIGRATION_ID,
    [string]$SessionCookie = $env:DBMP_SESSION_COOKIE,
    [string]$CsrfToken = $env:DBMP_CSRF_TOKEN,
    [string]$Origin = $env:DBMP_ORIGIN,
    [Parameter(Mandatory = $true)][string]$TableName,
    [int]$InitialRows = 2000000,
    [int]$BatchSize = 10000,
    [int]$MaximumRows = 25000000,
    [string]$EnvironmentLabel = 'dedicated test database',
    [string]$EvidencePath = 'batch-benchmark-result.json',
    [switch]$ConfirmTestDatabase,
    [switch]$ConfirmManagedTargetMatchesConnection
)

$ErrorActionPreference = 'Stop'

if (-not $ConfirmTestDatabase) {
    throw 'This script creates and drops a table. Use only a dedicated test database and pass -ConfirmTestDatabase.'
}
if (-not $ConfirmManagedTargetMatchesConnection) {
    throw 'Verify the psql connection is the same target configured by DatabaseId, then pass -ConfirmManagedTargetMatchesConnection.'
}
$requiredEnvironment = @{
    TARGET_DATABASE_URL = $ConnectionString
    DBMP_API_BASE_URL = $ApiBaseUrl
    DBMP_DATABASE_ID = $DatabaseId
    DBMP_MIGRATION_ID = $MigrationId
    DBMP_SESSION_COOKIE = $SessionCookie
    DBMP_CSRF_TOKEN = $CsrfToken
    DBMP_ORIGIN = $Origin
}
foreach ($required in $requiredEnvironment.GetEnumerator()) {
    if ([string]::IsNullOrWhiteSpace($required.Value)) {
        throw "Set $($required.Key) or pass its corresponding parameter."
    }
}
if ($TableName -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') {
    throw 'TableName must be a simple PostgreSQL identifier.'
}
if ($InitialRows -lt 1 -or $BatchSize -lt 1 -or $MaximumRows -lt $InitialRows) {
    throw 'InitialRows and BatchSize must be positive, and MaximumRows must be at least InitialRows.'
}

$psqlCommand = Get-Command psql -ErrorAction Stop
$tableCreated = $false

function Invoke-Psql {
    param([string[]]$Arguments)
    $output = & $psqlCommand.Source -w -X -v ON_ERROR_STOP=1 -d $ConnectionString @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw 'psql failed. Check that the dedicated PostgreSQL target is reachable and credentials are valid.'
    }
    return ($output -join "`n")
}

function Measure-Update {
    $query = "EXPLAIN (ANALYZE, TIMING OFF, SUMMARY ON) UPDATE $TableName SET value=value+1"
    $output = Invoke-Psql @('-A', '-t', '-c', $query)
    $matches = [regex]::Matches($output, 'Execution Time:\s*([0-9]+(?:\.[0-9]+)?)\s*ms')
    if ($matches.Count -eq 0) {
        throw 'Could not read UPDATE duration from EXPLAIN ANALYZE output.'
    }
    return [double]::Parse($matches[$matches.Count - 1].Groups[1].Value, [Globalization.CultureInfo]::InvariantCulture)
}

function Invoke-Api {
    param(
        [string]$Uri,
        [string]$Method = 'GET',
        [hashtable]$Headers = @{}
    )
    return Invoke-RestMethod -Uri $Uri -Method $Method -Headers $Headers -TimeoutSec 30
}

function ConvertTo-UtcDateTime {
    param([Parameter(Mandatory = $true)][object]$Value)

    if ($Value -is [DateTimeOffset]) {
        return $Value.UtcDateTime
    }
    if ($Value -is [DateTime]) {
        return $Value.ToUniversalTime()
    }

    $parsed = [DateTimeOffset]::MinValue
    $styles = [Globalization.DateTimeStyles]::AssumeUniversal -bor [Globalization.DateTimeStyles]::AdjustToUniversal
    if ([DateTimeOffset]::TryParse([string]$Value, [Globalization.CultureInfo]::InvariantCulture, $styles, [ref]$parsed)) {
        return $parsed.UtcDateTime
    }
    throw "Could not parse API timestamp '$Value'."
}

$evidence = $null
try {
    $apiRoot = $ApiBaseUrl.TrimEnd('/')
    $headers = @{
        Cookie = "dbmp_session=$SessionCookie"
        'X-CSRF-Token' = $CsrfToken
        Origin = $Origin
    }
    $migration = Invoke-Api -Uri "$apiRoot/api/v1/databases/$DatabaseId/migrations/$MigrationId" -Headers $headers
    if ($migration.executionMode -ne 'batch' -or $migration.batchSize -ne $BatchSize -or
        $migration.batchCursorStart -ne 0 -or
        $migration.sql -notmatch [regex]::Escape($TableName) -or
        $migration.batchSelectSql -notmatch [regex]::Escape($TableName)) {
        throw 'Migration must be an approved batch definition for TableName and the requested BatchSize.'
    }

    [void](Invoke-Psql @('-c', "CREATE TABLE $TableName (id BIGINT PRIMARY KEY, value BIGINT NOT NULL)"))
    $tableCreated = $true
    $rowCount = $InitialRows
    [void](Invoke-Psql @('-c', "INSERT INTO $TableName (id,value) SELECT id,0 FROM generate_series(1,$rowCount) AS id"))

    $nonBatchMilliseconds = Measure-Update
    [void](Invoke-Psql @('-c', "UPDATE $TableName SET value=0"))
    while ($nonBatchMilliseconds -le 1000 -and $rowCount -lt $MaximumRows) {
        $nextRowCount = [Math]::Min($rowCount + $InitialRows, $MaximumRows)
        [void](Invoke-Psql @('-c', "INSERT INTO $TableName (id,value) SELECT id,0 FROM generate_series($($rowCount + 1),$nextRowCount) AS id"))
        $rowCount = $nextRowCount
        $nonBatchMilliseconds = Measure-Update
        [void](Invoke-Psql @('-c', "UPDATE $TableName SET value=0"))
    }
    if ($nonBatchMilliseconds -le 1000) {
        throw "A full-table UPDATE stayed below one second at $rowCount rows. Raise -MaximumRows and rerun."
    }

    $actionUri = "$apiRoot/api/v1/databases/$DatabaseId/migrations/$MigrationId/execute"
    $headers['Idempotency-Key'] = [Guid]::NewGuid().ToString()
    $applicationStopwatch = [Diagnostics.Stopwatch]::StartNew()
    $queued = Invoke-WebRequest -Uri $actionUri -Method Post -Headers $headers -ContentType 'application/json' -Body ''
    if ($queued.StatusCode -ne 202) {
        throw "Migration enqueue returned HTTP $($queued.StatusCode); expected 202 Accepted."
    }
    $job = $queued.Content | ConvertFrom-Json
    if ([string]::IsNullOrWhiteSpace($job.jobId)) {
        throw 'Accepted response did not contain a jobId.'
    }
    $jobLocationHeader = $queued.Headers['Location']
    if ($jobLocationHeader -is [System.Array]) {
        $jobLocationHeader = $jobLocationHeader[0]
    }
    $jobLocation = [string]$jobLocationHeader
    if ([string]::IsNullOrWhiteSpace($jobLocation)) {
        throw 'Accepted response did not contain a Location header.'
    }
    $jobUri = [Uri]::new([Uri]$apiRoot, $jobLocation)

    $deadline = [DateTime]::UtcNow.AddMinutes(15)
    do {
        Start-Sleep -Milliseconds 500
        $job = Invoke-Api -Uri $jobUri.AbsoluteUri -Headers $headers
        if ($job.status -in @('succeeded', 'failed', 'partial', 'partially_reverted', 'needs_reconciliation')) {
            break
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    $applicationStopwatch.Stop()
    if ($job.status -ne 'succeeded') {
        throw "Batch migration job ended as '$($job.status)'. Inspect job $($job.jobId) before rerunning."
    }

    if ([string]::IsNullOrWhiteSpace($job.executionId)) {
        throw 'Succeeded job did not contain an executionId.'
    }
    $progress = Invoke-Api -Uri "$apiRoot/api/v1/databases/$DatabaseId/migration-executions/$($job.executionId)/progress" -Headers $headers
    $completedMigration = Invoke-Api -Uri "$apiRoot/api/v1/databases/$DatabaseId/migrations/$MigrationId" -Headers $headers
    $verification = Invoke-Psql @('-A', '-t', '-F', '|', '-c', "SELECT count(*),count(*) FILTER (WHERE value=1),count(*) FILTER (WHERE value<>1) FROM $TableName")
    $counts = $verification.Trim().Split('|') | ForEach-Object { [long]::Parse($_, [Globalization.CultureInfo]::InvariantCulture) }
    if ($counts.Count -ne 3 -or $counts[0] -ne $rowCount -or $counts[1] -ne $rowCount -or $counts[2] -ne 0) {
        throw "Application batch result did not match the baseline fixture: '$verification'."
    }
    $finishedAtUtc = ConvertTo-UtcDateTime $completedMigration.finishedAt
    $startedAtUtc = ConvertTo-UtcDateTime $completedMigration.startedAt
    $executionDurationMilliseconds = ($finishedAtUtc - $startedAtUtc).TotalMilliseconds

    $evidence = [pscustomobject]@{
        environment = $EnvironmentLabel
        measuredAtUtc = [DateTime]::UtcNow.ToString('o')
        databaseId = $DatabaseId
        migrationId = $MigrationId
        jobId = $job.jobId
        executionId = $job.executionId
        jobStatus = $job.status
        targetRows = $rowCount
        batchSize = $BatchSize
        committedBatches = $progress.committedBatches
        committedRows = $progress.committedRows
        nonBatchUpdateMilliseconds = [Math]::Round($nonBatchMilliseconds, 3)
        applicationExecutionMilliseconds = [Math]::Round($executionDurationMilliseconds, 3)
        enqueueToTerminalMilliseconds = [Math]::Round($applicationStopwatch.Elapsed.TotalMilliseconds, 3)
        nonBatchAboveOneSecond = ($nonBatchMilliseconds -gt 1000)
        verifiedRows = $counts[1]
        sameFixtureVerified = $true
        targetValuesVerified = $true
    }
    $evidence | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $EvidencePath -Encoding utf8
    $evidence | ConvertTo-Json -Depth 4
}
finally {
    if ($tableCreated) {
        try {
            [void](Invoke-Psql @('-c', "DROP TABLE IF EXISTS $TableName"))
        }
        catch {
            Write-Warning "Benchmark fixture cleanup failed for table '$TableName'. Review the dedicated test database."
        }
    }
}
