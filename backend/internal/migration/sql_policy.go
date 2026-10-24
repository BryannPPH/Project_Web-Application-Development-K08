package migration

import (
	"fmt"
	"strings"
	"unicode"
)

func validateSQLPolicy(sqlText string) error {
	statements, err := topLevelStatementWords(sqlText)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSQLPolicy, err)
	}
	for _, words := range statements {
		if transactionControl(words) {
			return fmt.Errorf("%w: transaction control is managed by the server", ErrSQLPolicy)
		}
	}
	return nil
}

func validateMySQLAtomicPolicy(sqlText string) error {
	if hasMySQLExecutableComment(sqlText) {
		return fmt.Errorf("%w: MySQL executable comments are not permitted", ErrSQLPolicy)
	}
	statements, err := topLevelStatementWords(sqlText)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSQLPolicy, err)
	}
	if len(statements) == 0 {
		return ErrSQLPolicy
	}
	for _, words := range statements {
		if len(words) == 0 {
			continue
		}
		switch words[0] {
		case "INSERT", "UPDATE", "DELETE", "SELECT":
		default:
			return fmt.Errorf("%w: MySQL atomic mode only permits transactional DML", ErrSQLPolicy)
		}
	}
	return nil
}

func hasMySQLExecutableComment(input string) bool {
	quote := byte(0)
	lineComment := false
	blockComment := false
	for index := 0; index < len(input); index++ {
		current := input[index]
		if lineComment {
			if current == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if current == '*' && index+1 < len(input) && input[index+1] == '/' {
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
				if index+1 < len(input) && input[index+1] == quote {
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
			if index+1 < len(input) && input[index+1] == '-' {
				lineComment = true
				index++
			}
		case '/':
			if index+1 < len(input) && input[index+1] == '*' {
				if index+2 < len(input) && input[index+2] == '!' {
					return true
				}
				blockComment = true
				index++
			}
		}
	}
	return false
}

func transactionControl(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "BEGIN", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT":
		return true
	case "START":
		return len(words) > 1 && words[1] == "TRANSACTION"
	case "RELEASE":
		return len(words) > 1 && words[1] == "SAVEPOINT"
	case "PREPARE":
		return len(words) > 1 && words[1] == "TRANSACTION"
	case "SET":
		return (len(words) > 1 && words[1] == "TRANSACTION") ||
			(len(words) > 3 && words[1] == "SESSION" && words[2] == "CHARACTERISTICS" && words[3] == "AS")
	default:
		return false
	}
}

func topLevelStatementWords(input string) ([][]string, error) {
	var statements [][]string
	words := make([]string, 0, 4)
	for index := 0; index < len(input); {
		switch input[index] {
		case ';':
			if len(words) > 0 {
				statements = append(statements, words)
				words = make([]string, 0, 4)
			}
			index++
		case '\'', '"', '`':
			next, err := skipQuoted(input, index, input[index])
			if err != nil {
				return nil, err
			}
			index = next
		case '-':
			if index+1 < len(input) && input[index+1] == '-' {
				index += 2
				for index < len(input) && input[index] != '\n' {
					index++
				}
			} else {
				index++
			}
		case '#':
			index++
			for index < len(input) && input[index] != '\n' {
				index++
			}
		case '/':
			if index+1 < len(input) && input[index+1] == '*' {
				next, err := skipBlockComment(input, index)
				if err != nil {
					return nil, err
				}
				index = next
			} else {
				index++
			}
		case '$':
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
			if isIdentifierStart(rune(input[index])) {
				start := index
				index++
				for index < len(input) && isIdentifierPart(rune(input[index])) {
					index++
				}
				if len(words) < 4 {
					words = append(words, strings.ToUpper(input[start:index]))
				}
			} else {
				index++
			}
		}
	}
	if len(words) > 0 {
		statements = append(statements, words)
	}
	return statements, nil
}

func skipQuoted(input string, start int, quote byte) (int, error) {
	for index := start + 1; index < len(input); index++ {
		if input[index] == '\\' && quote != '"' {
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

func skipBlockComment(input string, start int) (int, error) {
	depth := 1
	for index := start + 2; index < len(input)-1; index++ {
		if input[index:index+2] == "/*" {
			depth++
			index++
			continue
		}
		if input[index:index+2] == "*/" {
			depth--
			index++
			if depth == 0 {
				return index + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated block comment")
}

func skipDollarQuote(input string, start int) (int, bool, error) {
	end := start + 1
	for end < len(input) && (isIdentifierPart(rune(input[end])) || input[end] == '$') {
		if input[end] == '$' {
			break
		}
		end++
	}
	if end >= len(input) || input[end] != '$' {
		return start, false, nil
	}
	tag := input[start : end+1]
	closing := strings.Index(input[end+1:], tag)
	if closing < 0 {
		return 0, true, fmt.Errorf("unterminated dollar-quoted value")
	}
	return end + 1 + closing + len(tag), true, nil
}

func isIdentifierStart(value rune) bool { return value == '_' || unicode.IsLetter(value) }
func isIdentifierPart(value rune) bool {
	return isIdentifierStart(value) || unicode.IsDigit(value) || value == '$'
}
