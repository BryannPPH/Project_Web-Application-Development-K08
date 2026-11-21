param(
    [string]$ConnectionString = $env:TARGET_DATABASE_URL,
    [int]$InitialRows = 2000000,
    [int]$BatchSize = 10000,
    [int]$MaximumRows = 25000000
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($ConnectionString)) {
    throw 'Pass -ConnectionString or set TARGET_DATABASE_URL.'
}
if ($InitialRows -lt 1 -or $BatchSize -lt 1 -or $MaximumRows -lt $InitialRows) {
    throw 'InitialRows and BatchSize must be positive, and MaximumRows must be at least InitialRows.'
}

$psqlCommand = Get-Command psql -ErrorAction Stop
$tableName = 'dbmp_batch_bench_' + [Guid]::NewGuid().ToString('N').Substring(0, 16)
$temporarySql = Join-Path ([System.IO.Path]::GetTempPath()) ($tableName + '.sql')
$rowCount = $InitialRows
$hadPgDatabase = Test-Path Env:PGDATABASE
$previousPgDatabase = $env:PGDATABASE
$env:PGDATABASE = $ConnectionString

function Invoke-Psql {
    param([string[]]$Arguments)
    $output = & $psqlCommand.Source -w @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw 'psql failed. Check that the PostgreSQL target is reachable and credentials are valid.'
    }
    return ($output -join "`n")
}

function Measure-Update {
    param([long]$Rows)
    $query = "EXPLAIN (ANALYZE, TIMING OFF, SUMMARY ON) UPDATE $tableName SET value=value+1"
    $output = Invoke-Psql @('-X', '-v', 'ON_ERROR_STOP=1', '-A', '-t', '-c', $query)
    $matches = [regex]::Matches($output, 'Execution Time:\s*([0-9]+(?:\.[0-9]+)?)\s*ms')
    if ($matches.Count -eq 0) {
        throw 'Could not read UPDATE duration from EXPLAIN ANALYZE output.'
    }
    return [double]::Parse($matches[$matches.Count - 1].Groups[1].Value, [Globalization.CultureInfo]::InvariantCulture)
}

try {
    [void](Invoke-Psql @('-X', '-v', 'ON_ERROR_STOP=1', '-c', "CREATE TABLE $tableName (id BIGINT PRIMARY KEY, value BIGINT NOT NULL)"))
    [void](Invoke-Psql @('-X', '-v', 'ON_ERROR_STOP=1', '-c', "INSERT INTO $tableName (id,value) SELECT id,0 FROM generate_series(1,$rowCount) AS id"))

    $nonBatchMilliseconds = Measure-Update $rowCount
    while ($nonBatchMilliseconds -le 1000 -and $rowCount -lt $MaximumRows) {
        $nextRowCount = [Math]::Min($rowCount + $InitialRows, $MaximumRows)
        [void](Invoke-Psql @('-X', '-v', 'ON_ERROR_STOP=1', '-c', "INSERT INTO $tableName (id,value) SELECT id,0 FROM generate_series($($rowCount + 1),$nextRowCount) AS id"))
        $rowCount = $nextRowCount
        $nonBatchMilliseconds = Measure-Update $rowCount
    }
    if ($nonBatchMilliseconds -le 1000) {
        throw "A full-table UPDATE stayed below one second at $rowCount rows. Raise -MaximumRows and rerun."
    }

    $script = [System.Text.StringBuilder]::new()
    [void]$script.AppendLine('\set ON_ERROR_STOP on')
    $batchNumber = 0
    for ($cursorStart = 0; $cursorStart -lt $rowCount; $cursorStart += $BatchSize) {
        $batchNumber++
        [void]$script.AppendLine('BEGIN;')
        [void]$script.AppendLine("SELECT COALESCE(MAX(id),$cursorStart) AS cursor_end FROM (SELECT id FROM $tableName WHERE id > $cursorStart ORDER BY id LIMIT $BatchSize) selected \gset")
        [void]$script.AppendLine("UPDATE $tableName SET value=value+1 WHERE id > $cursorStart AND id <= :cursor_end;")
        [void]$script.AppendLine('COMMIT;')
    }
    [System.IO.File]::WriteAllText($temporarySql, $script.ToString(), [System.Text.UTF8Encoding]::new($false))
    $batchStopwatch = [Diagnostics.Stopwatch]::StartNew()
    [void](Invoke-Psql @('-X', '-f', $temporarySql))
    $batchStopwatch.Stop()
    $verifiedRows = [int](Invoke-Psql @('-X', '-A', '-t', '-c', "SELECT count(*) FROM $tableName WHERE value=2"))
    if ($verifiedRows -ne $rowCount) {
        throw "Batch UPDATE affected $verifiedRows of $rowCount rows."
    }

    [pscustomobject]@{
        targetRows = $rowCount
        batchSize = $BatchSize
        batchCount = $batchNumber
        nonBatchUpdateMilliseconds = [Math]::Round($nonBatchMilliseconds, 3)
        batchUpdateAndTransactionsMilliseconds = [Math]::Round($batchStopwatch.Elapsed.TotalMilliseconds, 3)
        nonBatchAboveOneSecond = $true
        datasetVerified = $true
        measuredAtUtc = [DateTime]::UtcNow.ToString('o')
    } | ConvertTo-Json -Depth 3
}
finally {
    if (Test-Path -LiteralPath $temporarySql) {
        Remove-Item -LiteralPath $temporarySql -Force
    }
    try {
        [void](Invoke-Psql @('-X', '-v', 'ON_ERROR_STOP=1', '-c', "DROP TABLE IF EXISTS $tableName"))
    }
    catch {
        Write-Warning 'Benchmark fixture cleanup failed; review the generated table name in this script run.'
    }
    if ($hadPgDatabase) {
        $env:PGDATABASE = $previousPgDatabase
    }
    else {
        Remove-Item Env:PGDATABASE -ErrorAction SilentlyContinue
    }
}
