package targetdb

import (
	"regexp"
	"strings"
)

var (
	// Match the literal alone so adjacent literals never share a delimiter.
	sqlNumber      = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F_]+|0[bB][01_]+|0[oO][0-7_]+|(?:[0-9][0-9_]*(?:\.[0-9_]*)?|\.[0-9][0-9_]*)(?:[eE][+-]?[0-9][0-9_]*)?)`)
	sqlCredential  = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|session)\s*=\s*[^\s,;()]+`)
	sqlParameter   = regexp.MustCompile(`^\$[0-9]+$`)
	sqlPostgresURL = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/@\s]+:)[^@\s]+@`)
)

// RedactSQLStatement is for callers that do not know the dialect. In that
// case double quotes are treated as sensitive, since MySQL accepts them for strings.
func RedactSQLStatement(query string) string {
	return redactSQLStatement(query, "")
}

func redactSQLStatement(query string, engine Engine) string {
	var safe strings.Builder
	for i := 0; i < len(query); {
		rest := query[i:]
		switch {
		case strings.HasPrefix(rest, "--") || rest[0] == '#' && engine != EnginePostgreSQL:
			safe.WriteString("[comment]")
			end := strings.IndexAny(rest, "\r\n")
			if end < 0 {
				i = len(query)
			} else {
				i += end
			}
		case strings.HasPrefix(rest, "/*"):
			end, ok := sqlBlockCommentEnd(rest)
			if !ok {
				return "[redacted]"
			}
			safe.WriteString("[comment]")
			i += end
		case rest[0] == '\'' || rest[0] == '"' || rest[0] == '`':
			quote := rest[0]
			preserveIdentifier := quote == '`' && engine == EngineMySQL ||
				quote == '"' && engine == EnginePostgreSQL
			end, ok := sqlQuotedEnd(rest, preserveIdentifier)
			if !ok {
				return "[redacted]"
			}
			if preserveIdentifier {
				safe.WriteString(rest[:end])
			} else {
				safe.WriteString("'[redacted]'")
			}
			i += end
		case rest[0] == '$':
			if tag := sqlDollarTag(rest); tag != "" {
				end := strings.Index(rest[len(tag):], tag)
				if end < 0 {
					return "[redacted]"
				}
				safe.WriteString("'[redacted]'")
				i += len(tag)*2 + end
			} else {
				// PostgreSQL positional parameters are not literal values.
				end := 1
				for end < len(rest) && sqlDigit(rest[end]) {
					end++
				}
				safe.WriteString(rest[:end])
				i += end
			}
		case sqlWordStart(rest[0]):
			end := 1
			for end < len(rest) && (sqlWordStart(rest[end]) || sqlDigit(rest[end]) || rest[end] == '$') {
				end++
			}
			safe.WriteString(rest[:end])
			i += end
		case sqlDigit(rest[0]) || len(rest) > 1 && rest[0] == '.' && sqlDigit(rest[1]):
			end := sqlNumber.FindStringIndex(rest)[1]
			safe.WriteString("[redacted]")
			i += end
		default:
			safe.WriteByte(rest[0])
			i++
		}
	}
	redacted := sqlCredential.ReplaceAllStringFunc(safe.String(), func(assignment string) string {
		key, value, _ := strings.Cut(assignment, "=")
		value = strings.TrimSpace(value)
		if value == "?" || sqlParameter.MatchString(value) {
			return assignment
		}
		return strings.TrimSpace(key) + "=[redacted]"
	})
	return sqlPostgresURL.ReplaceAllString(redacted, "$1[redacted]@")
}

func sqlDigit(c byte) bool { return c >= '0' && c <= '9' }

func sqlWordStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func sqlDollarTag(input string) string {
	if len(input) > 1 && input[1] == '$' {
		return "$$"
	}
	if len(input) < 3 || !sqlWordStart(input[1]) {
		return ""
	}
	for i := 2; i < len(input); i++ {
		if input[i] == '$' {
			return input[:i+1]
		}
		if !sqlWordStart(input[i]) && !sqlDigit(input[i]) {
			break
		}
	}
	return ""
}

func sqlQuotedEnd(input string, identifier bool) (int, bool) {
	quote := input[0]
	for i := 1; i < len(input); i++ {
		if !identifier && input[i] == '\\' {
			start := i
			for i < len(input) && input[i] == '\\' {
				i++
			}
			// SQL session settings can change the meaning of an escaped quote.
			// Omit the statement if the terminator could be read either way.
			if i < len(input) && input[i] == quote && (i-start)%2 != 0 {
				return 0, false
			}
			i--
			continue
		}
		if input[i] != quote {
			continue
		}
		if i+1 < len(input) && input[i+1] == quote {
			i++
			continue
		}
		return i + 1, true
	}
	return 0, false
}

func sqlBlockCommentEnd(input string) (int, bool) {
	depth := 1
	for i := 2; i+1 < len(input); i++ {
		switch input[i : i+2] {
		case "/*":
			depth++
			i++
		case "*/":
			depth--
			if depth == 0 {
				return i + 2, true
			}
			i++
		}
	}
	return 0, false
}
