package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// Query execution plan analysis (bonus): menjalankan EXPLAIN / EXPLAIN ANALYZE
// terhadap SQL migration SEBELUM migration dieksekusi.
//
// Seluruh analisis berjalan di dalam satu transaksi yang SELALU di-rollback,
// dengan savepoint per statement. Akibatnya:
//   - EXPLAIN ANALYZE (yang benar-benar mengeksekusi statement) tidak pernah
//     meninggalkan perubahan pada database target;
//   - statement DDL tanpa execution plan dijalankan sementara agar statement
//     berikutnya (mis. INSERT ke table yang baru dibuat) tetap dapat dianalisis;
//   - kegagalan satu statement tidak menghentikan analisis statement lain.

const (
	// MaxExplainStatements membatasi jumlah statement per analisis.
	MaxExplainStatements    = 50
	defaultExplainTimeout   = 30 * time.Second
	explainStatementTimeout = "10s"
	explainLockTimeout      = "3s"
	explainSavepoint        = "dbm_explain"
)

// StatementKind menjelaskan apa yang terjadi pada satu statement.
type StatementKind string

const (
	// StatementPlanned: execution plan berhasil didapat.
	StatementPlanned StatementKind = "plan"
	// StatementExecuted: statement tanpa plan (DDL/utility) dijalankan sementara.
	StatementExecuted StatementKind = "executed"
	// StatementFailed: statement atau EXPLAIN-nya gagal.
	StatementFailed StatementKind = "error"
)

// StatementPlan adalah hasil analisis satu statement.
type StatementPlan struct {
	Index   int           `json:"index"`
	SQL     string        `json:"sql"`
	Kind    StatementKind `json:"kind"`
	Note    string        `json:"note,omitempty"`
	Error   string        `json:"error,omitempty"`
	Plan    *PlanNode     `json:"plan,omitempty"`
	Summary *PlanSummary  `json:"summary,omitempty"`
	Raw     string        `json:"raw,omitempty"`
}

// ExplainResult adalah response analisis untuk seluruh SQL migration.
type ExplainResult struct {
	Engine     targetdb.Engine `json:"engine"`
	Analyze    bool            `json:"analyze"`
	RolledBack bool            `json:"rolledBack"`
	Statements []StatementPlan `json:"statements"`
}

// ExplainService menjalankan analisis plan. Hanya user View + Edit yang boleh,
// karena EXPLAIN ANALYZE dan DDL sementara benar-benar dieksekusi (lalu di-rollback).
type ExplainService struct {
	authorizer  Authorizer
	provider    targetdb.Provider
	maxSQLBytes int
	timeout     time.Duration
}

func NewExplainService(authorizer Authorizer, provider targetdb.Provider, config ServiceConfig) (*ExplainService, error) {
	if authorizer == nil || provider == nil {
		return nil, errors.New("explain service requires authorizer and target provider")
	}
	maxSQLBytes := config.MaxSQLBytes
	if maxSQLBytes == 0 {
		maxSQLBytes = DefaultMaxSQLBytes
	}
	return &ExplainService{authorizer: authorizer, provider: provider, maxSQLBytes: maxSQLBytes, timeout: defaultExplainTimeout}, nil
}

func (s *ExplainService) Explain(ctx context.Context, principal auth.Principal, databaseID, sqlText string, analyze bool) (ExplainResult, error) {
	if strings.TrimSpace(sqlText) == "" || len(sqlText) > s.maxSQLBytes {
		return ExplainResult{}, ErrInvalidInput
	}
	// Authorization selalu sebelum koneksi target dibuka.
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return ExplainResult{}, err
	}
	if err := validateSQLPolicy(sqlText); err != nil {
		return ExplainResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	connection, err := s.provider.Open(ctx, databaseID)
	if err != nil {
		return ExplainResult{}, err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()

	engine := connection.Engine()
	if engine == targetdb.EngineMySQL {
		if err := validateMySQLAtomicPolicy(sqlText); err != nil {
			return ExplainResult{}, err
		}
	}
	statements, err := splitStatements(sqlText, engine)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("%w: %v", ErrSQLPolicy, err)
	}
	if len(statements) == 0 || len(statements) > MaxExplainStatements {
		return ExplainResult{}, ErrInvalidInput
	}

	transaction, err := connection.Begin(ctx)
	if err != nil {
		return ExplainResult{}, err
	}
	// Rollback dijalankan di semua jalur: analisis tidak pernah meninggalkan perubahan.
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if engine == targetdb.EnginePostgreSQL {
		for _, setting := range [][2]string{{"statement_timeout", explainStatementTimeout}, {"lock_timeout", explainLockTimeout}} {
			if _, err := transaction.Exec(ctx, "SELECT set_config($1, $2, true)", setting[0], setting[1]); err != nil {
				return ExplainResult{}, err
			}
		}
	}

	result := ExplainResult{Engine: engine, Analyze: analyze, RolledBack: true, Statements: make([]StatementPlan, 0, len(statements))}
	for index, statement := range statements {
		plan := StatementPlan{Index: index + 1, SQL: statement}
		if _, err := transaction.Exec(ctx, "SAVEPOINT "+explainSavepoint); err != nil {
			return ExplainResult{}, err
		}
		var runErr error
		if engine == targetdb.EngineMySQL {
			runErr = explainMySQLStatement(ctx, transaction, statement, analyze, &plan)
		} else {
			runErr = explainPostgresStatement(ctx, transaction, statement, analyze, &plan)
		}
		if runErr != nil {
			plan.Kind = StatementFailed
			plan.Error = sanitizeTargetError(runErr)
			plan.Plan, plan.Summary, plan.Raw = nil, nil, ""
			// Kembalikan transaksi ke kondisi sebelum statement agar analisis berlanjut.
			if _, err := transaction.Exec(ctx, "ROLLBACK TO SAVEPOINT "+explainSavepoint); err != nil {
				return ExplainResult{}, err
			}
		} else if _, err := transaction.Exec(ctx, "RELEASE SAVEPOINT "+explainSavepoint); err != nil {
			return ExplainResult{}, err
		}
		result.Statements = append(result.Statements, plan)
	}
	return result, nil
}

// ---------- PostgreSQL ----------

// postgresExplainable: statement yang dapat diberi EXPLAIN oleh PostgreSQL.
var postgresExplainable = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	"VALUES": true, "WITH": true, "TABLE": true, "EXECUTE": true,
}

func explainPostgresStatement(ctx context.Context, tx targetdb.Transaction, statement string, analyze bool, plan *StatementPlan) error {
	if !postgresExplainable[firstKeyword(statement)] {
		// DDL/utility: tidak punya execution plan; dijalankan sementara saja.
		started := time.Now()
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
		plan.Kind = StatementExecuted
		plan.Note = executedNote(time.Since(started))
		return nil
	}
	options := "FORMAT JSON"
	if analyze {
		options = "ANALYZE, BUFFERS, FORMAT JSON"
	}
	var raw string
	if err := tx.QueryRow(ctx, "EXPLAIN ("+options+") "+statement).Scan(&raw); err != nil {
		return err
	}
	root, summary, err := parsePostgresPlan([]byte(raw))
	if err != nil {
		return err
	}
	plan.Kind, plan.Plan, plan.Summary, plan.Raw = StatementPlanned, root, &summary, prettyJSON([]byte(raw))
	return nil
}

// ---------- MySQL ----------

func explainMySQLStatement(ctx context.Context, tx targetdb.Transaction, statement string, analyze bool, plan *StatementPlan) error {
	keyword := firstKeyword(statement)
	if analyze {
		// EXPLAIN ANALYZE MySQL mendukung SELECT (dan beberapa UPDATE/DELETE).
		text, err := queryText(ctx, tx, "EXPLAIN ANALYZE "+statement)
		if err == nil && usableMySQLTree(text) {
			if err := applyMySQLTree(plan, text); err != nil {
				return err
			}
			if plan.Plan.ActualTotalMs != nil {
				return nil
			}
			// Tree tanpa angka aktual (mis. INSERT ... VALUES). Statement mungkin
			// sudah tereksekusi, jadi kembalikan ke savepoint sebelum diukur ulang.
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+explainSavepoint); err != nil {
				return err
			}
		} else if err := explainMySQLEstimate(ctx, tx, statement, plan); err != nil {
			// Tidak didukung: pakai plan estimasi lalu ukur eksekusi langsung.
			return err
		}
		started := time.Now()
		var affected int64
		if keyword == "SELECT" {
			rows, err := tx.Query(ctx, statement)
			if err != nil {
				return err
			}
			for rows.Next() {
				affected++
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		} else {
			result, err := tx.Exec(ctx, statement)
			if err != nil {
				return err
			}
			affected = result.RowsAffected()
		}
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		rows := float64(affected)
		plan.Summary.ExecutionMs, plan.Summary.ActualRows = &elapsed, &rows
		plan.Note = "MySQL tidak mendukung EXPLAIN ANALYZE untuk statement ini; waktu dan jumlah row diukur dari eksekusi sementara yang kemudian di-rollback."
		return nil
	}
	return explainMySQLEstimate(ctx, tx, statement, plan)
}

// explainMySQLEstimate mencoba FORMAT=TREE, lalu EXPLAIN tradisional.
func explainMySQLEstimate(ctx context.Context, tx targetdb.Transaction, statement string, plan *StatementPlan) error {
	if text, err := queryText(ctx, tx, "EXPLAIN FORMAT=TREE "+statement); err == nil && usableMySQLTree(text) {
		return applyMySQLTree(plan, text)
	}
	rows, err := tx.Query(ctx, "EXPLAIN "+statement)
	if err != nil {
		return err
	}
	defer rows.Close()
	var table [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}
		table = append(table, values)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	root, summary, err := parseMySQLTabular(table)
	if err != nil {
		return err
	}
	plan.Kind, plan.Plan, plan.Summary, plan.Raw = StatementPlanned, root, &summary, tabularText(table)
	return nil
}

func applyMySQLTree(plan *StatementPlan, text string) error {
	root, summary, err := parseMySQLTree(text)
	if err != nil {
		return err
	}
	plan.Kind, plan.Plan, plan.Summary, plan.Raw = StatementPlanned, root, &summary, text
	return nil
}

// usableMySQLTree: untuk UPDATE/DELETE satu table MySQL tidak memberi error,
// melainkan placeholder "<not executable by iterator executor>".
func usableMySQLTree(text string) bool {
	return strings.TrimSpace(text) != "" && !strings.Contains(text, "not executable by iterator executor")
}

// queryText membaca hasil EXPLAIN satu kolom (bisa beberapa baris).
func queryText(ctx context.Context, tx targetdb.Transaction, query string) (string, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return "", err
		}
		if len(values) > 0 {
			lines = append(lines, textCell(values[0]))
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func executedNote(elapsed time.Duration) string {
	return fmt.Sprintf("Statement DDL/utility tidak memiliki execution plan. Statement dijalankan sementara (%.1f ms) agar statement berikutnya dapat dianalisis, lalu di-rollback.",
		float64(elapsed.Microseconds())/1000)
}

// ---------- Pemisah statement ----------

// splitStatements memecah script menjadi statement top-level dengan aturan
// leksikal sesuai engine: dollar quote dan komentar bersarang untuk PostgreSQL;
// komentar '#' dan escape backslash untuk MySQL.
func splitStatements(input string, engine targetdb.Engine) ([]string, error) {
	mysql := engine == targetdb.EngineMySQL
	var statements []string
	start := 0
	flush := func(end int) {
		if statement := strings.TrimSpace(input[start:end]); statement != "" && !onlyComments(statement, mysql) {
			statements = append(statements, statement)
		}
	}
	for index := 0; index < len(input); {
		switch current := input[index]; {
		case current == ';':
			flush(index)
			index++
			start = index
		case current == '\'' || current == '"' || current == '`':
			next, err := skipQuotedFor(input, index, mysql)
			if err != nil {
				return nil, err
			}
			index = next
		case current == '-' && index+1 < len(input) && input[index+1] == '-':
			index = skipLine(input, index)
		case current == '#' && mysql:
			index = skipLine(input, index)
		case current == '/' && index+1 < len(input) && input[index+1] == '*':
			next, err := skipBlockComment(input, index)
			if err != nil {
				return nil, err
			}
			index = next
		case current == '$' && !mysql:
			next, ok, err := skipDollarQuote(input, index)
			if err != nil {
				return nil, err
			}
			if ok {
				index = next
			} else {
				index++
			}
		default:
			index++
		}
	}
	flush(len(input))
	return statements, nil
}

func skipQuotedFor(input string, start int, mysql bool) (int, error) {
	quote := input[start]
	for index := start + 1; index < len(input); index++ {
		// PostgreSQL (standard_conforming_strings) tidak memakai backslash escape.
		if mysql && input[index] == '\\' && quote != '`' {
			index++
			continue
		}
		if input[index] != quote {
			continue
		}
		if index+1 < len(input) && input[index+1] == quote {
			index++
			continue
		}
		return index + 1, nil
	}
	return 0, fmt.Errorf("unterminated quoted value")
}

func skipLine(input string, index int) int {
	for index < len(input) && input[index] != '\n' {
		index++
	}
	return index
}

// onlyComments bernilai true bila potongan hanya berisi komentar.
func onlyComments(statement string, mysql bool) bool {
	return firstKeywordFor(statement, mysql) == ""
}

// firstKeyword mengembalikan kata kunci pertama statement (huruf besar).
func firstKeyword(statement string) string {
	return firstKeywordFor(statement, true)
}

func firstKeywordFor(statement string, hashComments bool) string {
	for index := 0; index < len(statement); {
		current := statement[index]
		switch {
		case current == '-' && index+1 < len(statement) && statement[index+1] == '-':
			index = skipLine(statement, index)
		case current == '#' && hashComments:
			index = skipLine(statement, index)
		case current == '/' && index+1 < len(statement) && statement[index+1] == '*':
			end := strings.Index(statement[index+2:], "*/")
			if end < 0 {
				return ""
			}
			index += end + 4
		case current == '(' || current == ' ' || current == '\t' || current == '\n' || current == '\r':
			index++
		case isIdentifierStart(rune(current)):
			end := index + 1
			for end < len(statement) && isIdentifierPart(rune(statement[end])) {
				end++
			}
			return strings.ToUpper(statement[index:end])
		default:
			return ""
		}
	}
	return ""
}
