package targetdb

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func ConnectMySQL(ctx context.Context, config ConnectionConfig, timeout time.Duration) (Connection, error) {
	config.Engine = EngineMySQL
	if err := config.Validate(); err != nil {
		return nil, newUnavailableError(config.DatabaseID, err)
	}
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	driverConfig := mysqldriver.NewConfig()
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(strings.Trim(config.Host, "[]"), fmt.Sprint(config.Port))
	driverConfig.DBName = config.Database
	driverConfig.User = config.Username
	driverConfig.Passwd = config.Password
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Timeout = timeout
	driverConfig.ReadTimeout = timeout
	driverConfig.WriteTimeout = timeout
	if config.SSLMode == "require" {
		driverConfig.TLSConfig = "true"
	} else {
		driverConfig.TLSConfig = "false"
	}

	database, err := sql.Open("mysql", driverConfig.FormatDSN())
	driverConfig.Passwd = ""
	if err != nil {
		return nil, newUnavailableError(config.DatabaseID, err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := database.PingContext(connectCtx); err != nil {
		_ = database.Close()
		return nil, newUnavailableError(config.DatabaseID, err)
	}
	return &mysqlConnection{database: database}, nil
}

type mysqlConnection struct {
	database *sql.DB
}

func (*mysqlConnection) Engine() Engine { return EngineMySQL }

func (c *mysqlConnection) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := c.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return newMySQLRows(rows), nil
}

func (c *mysqlConnection) QueryRow(ctx context.Context, query string, args ...any) Row {
	return c.database.QueryRowContext(ctx, query, args...)
}

func (c *mysqlConnection) Exec(ctx context.Context, query string, args ...any) (CommandResult, error) {
	result, err := c.database.ExecContext(ctx, query, args...)
	return commandResult(result, err)
}

func (c *mysqlConnection) Begin(ctx context.Context, options ...TxOptions) (Transaction, error) {
	readOnly := len(options) > 0 && options[0].ReadOnly
	tx, err := c.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return nil, err
	}
	return &mysqlTransaction{tx: tx}, nil
}

func (c *mysqlConnection) Close(context.Context) error { return c.database.Close() }

type mysqlTransaction struct {
	tx *sql.Tx
}

func (t *mysqlTransaction) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return newMySQLRows(rows), nil
}

func (t *mysqlTransaction) QueryRow(ctx context.Context, query string, args ...any) Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *mysqlTransaction) Exec(ctx context.Context, query string, args ...any) (CommandResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	return commandResult(result, err)
}

func (t *mysqlTransaction) ExecScript(ctx context.Context, script string) (CommandResult, error) {
	statements, err := splitMySQLScript(script)
	if err != nil {
		return nil, err
	}
	var affected int64
	for _, statement := range statements {
		result, err := t.tx.ExecContext(ctx, statement)
		if err != nil {
			return nil, err
		}
		rows, err := result.RowsAffected()
		if err == nil {
			affected += rows
		}
	}
	return resultValue{affected: affected}, nil
}

func (t *mysqlTransaction) Commit(context.Context) error   { return t.tx.Commit() }
func (t *mysqlTransaction) Rollback(context.Context) error { return t.tx.Rollback() }

type resultValue struct{ affected int64 }

func (r resultValue) RowsAffected() int64 { return r.affected }

func commandResult(result sql.Result, err error) (CommandResult, error) {
	if err != nil {
		return nil, err
	}
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return nil, affectedErr
	}
	return resultValue{affected: affected}, nil
}

type mysqlRows struct {
	rows      *sql.Rows
	typeNames []string
}

func newMySQLRows(rows *sql.Rows) *mysqlRows {
	columnTypes, _ := rows.ColumnTypes()
	typeNames := make([]string, len(columnTypes))
	for index, columnType := range columnTypes {
		typeNames[index] = strings.ToUpper(columnType.DatabaseTypeName())
	}
	return &mysqlRows{rows: rows, typeNames: typeNames}
}

func (r *mysqlRows) Close()                 { _ = r.rows.Close() }
func (r *mysqlRows) Err() error             { return r.rows.Err() }
func (r *mysqlRows) Next() bool             { return r.rows.Next() }
func (r *mysqlRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }

func (r *mysqlRows) Values() ([]any, error) {
	values := make([]any, len(r.typeNames))
	destinations := make([]any, len(values))
	for index := range values {
		destinations[index] = &values[index]
	}
	if err := r.rows.Scan(destinations...); err != nil {
		return nil, err
	}
	for index, value := range values {
		bytes, ok := value.([]byte)
		if !ok {
			continue
		}
		copyOfBytes := append([]byte(nil), bytes...)
		if mysqlBinaryType(r.typeNames[index]) {
			values[index] = copyOfBytes
		} else {
			values[index] = string(copyOfBytes)
		}
	}
	return values, nil
}

func mysqlBinaryType(typeName string) bool {
	return strings.Contains(typeName, "BLOB") || strings.Contains(typeName, "BINARY") || typeName == "BIT"
}

func splitMySQLScript(script string) ([]string, error) {
	statements := make([]string, 0)
	start := 0
	quote := byte(0)
	lineComment := false
	blockComment := false
	for index := 0; index < len(script); index++ {
		current := script[index]
		if lineComment {
			if current == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if current == '*' && index+1 < len(script) && script[index+1] == '/' {
				blockComment = false
				index++
			}
			continue
		}
		if quote != 0 {
			if current == '\\' {
				index++
				continue
			}
			if current == quote {
				if index+1 < len(script) && script[index+1] == quote {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"', '`':
			quote = current
		case '#':
			lineComment = true
		case '-':
			if index+1 < len(script) && script[index+1] == '-' {
				lineComment = true
				index++
			}
		case '/':
			if index+1 < len(script) && script[index+1] == '*' {
				if index+2 < len(script) && script[index+2] == '!' {
					return nil, fmt.Errorf("MySQL executable comments are not permitted")
				}
				blockComment = true
				index++
			}
		case ';':
			statement := strings.TrimSpace(script[start:index])
			if statement != "" {
				statements = append(statements, statement)
			}
			start = index + 1
		}
	}
	if quote != 0 || blockComment {
		return nil, fmt.Errorf("unterminated MySQL SQL construct")
	}
	if statement := strings.TrimSpace(script[start:]); statement != "" {
		statements = append(statements, statement)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("empty MySQL SQL script")
	}
	return statements, nil
}
