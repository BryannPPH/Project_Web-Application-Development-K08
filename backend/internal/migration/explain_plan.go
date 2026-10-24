package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PlanNode adalah satu node execution plan yang sudah dinormalisasi sehingga
// PostgreSQL dan MySQL dapat ditampilkan dengan komponen frontend yang sama.
// Nilai actual* hanya terisi pada EXPLAIN ANALYZE.
type PlanNode struct {
	NodeType        string     `json:"nodeType"`
	Relation        string     `json:"relation,omitempty"`
	Alias           string     `json:"alias,omitempty"`
	Index           string     `json:"index,omitempty"`
	JoinType        string     `json:"joinType,omitempty"`
	Condition       string     `json:"condition,omitempty"`
	Filter          string     `json:"filter,omitempty"`
	StartupCost     *float64   `json:"startupCost,omitempty"`
	TotalCost       *float64   `json:"totalCost,omitempty"`
	PlanRows        *float64   `json:"planRows,omitempty"`
	ActualStartupMs *float64   `json:"actualStartupMs,omitempty"`
	ActualTotalMs   *float64   `json:"actualTotalMs,omitempty"`
	ActualRows      *float64   `json:"actualRows,omitempty"`
	ActualLoops     *float64   `json:"actualLoops,omitempty"`
	Children        []PlanNode `json:"children,omitempty"`
}

// PlanSummary merangkum hal yang diminta spesifikasi: jenis scan, penggunaan
// index, strategi join, estimasi cost, waktu eksekusi aktual, dan jumlah row.
type PlanSummary struct {
	TotalCost     *float64 `json:"totalCost,omitempty"`
	EstimatedRows *float64 `json:"estimatedRows,omitempty"`
	ActualRows    *float64 `json:"actualRows,omitempty"`
	PlanningMs    *float64 `json:"planningMs,omitempty"`
	ExecutionMs   *float64 `json:"executionMs,omitempty"`
	ScanTypes     []string `json:"scanTypes"`
	Indexes       []string `json:"indexes"`
	JoinTypes     []string `json:"joinTypes"`
	Relations     []string `json:"relations"`
	Warnings      []string `json:"warnings"`
}

// largeSeqScanRows adalah ambang estimasi row untuk peringatan full scan.
const largeSeqScanRows = 1000

// ---------- PostgreSQL: EXPLAIN (FORMAT JSON) ----------

type pgPlanJSON struct {
	Plan          map[string]any `json:"Plan"`
	PlanningTime  *float64       `json:"Planning Time"`
	ExecutionTime *float64       `json:"Execution Time"`
}

// parsePostgresPlan membaca output EXPLAIN (FORMAT JSON) PostgreSQL.
func parsePostgresPlan(raw []byte) (*PlanNode, PlanSummary, error) {
	var documents []pgPlanJSON
	if err := json.Unmarshal(raw, &documents); err != nil {
		return nil, PlanSummary{}, fmt.Errorf("decode postgres plan: %w", err)
	}
	if len(documents) == 0 || documents[0].Plan == nil {
		return nil, PlanSummary{}, fmt.Errorf("postgres plan is empty")
	}
	root := postgresNode(documents[0].Plan)
	summary := summarize(&root)
	summary.PlanningMs = documents[0].PlanningTime
	summary.ExecutionMs = documents[0].ExecutionTime
	return &root, summary, nil
}

func postgresNode(values map[string]any) PlanNode {
	node := PlanNode{
		NodeType:        stringValue(values["Node Type"]),
		Relation:        stringValue(values["Relation Name"]),
		Alias:           stringValue(values["Alias"]),
		Index:           stringValue(values["Index Name"]),
		JoinType:        stringValue(values["Join Type"]),
		StartupCost:     numberValue(values["Startup Cost"]),
		TotalCost:       numberValue(values["Total Cost"]),
		PlanRows:        numberValue(values["Plan Rows"]),
		ActualStartupMs: numberValue(values["Actual Startup Time"]),
		ActualTotalMs:   numberValue(values["Actual Total Time"]),
		ActualRows:      numberValue(values["Actual Rows"]),
		ActualLoops:     numberValue(values["Actual Loops"]),
		Filter:          stringValue(values["Filter"]),
	}
	if node.Alias == node.Relation {
		node.Alias = ""
	}
	// ModifyTable menyimpan operasinya di "Operation" (Insert/Update/Delete).
	if operation := stringValue(values["Operation"]); operation != "" && node.NodeType == "ModifyTable" {
		node.NodeType = operation
	}
	for _, key := range []string{"Index Cond", "Hash Cond", "Merge Cond", "Join Filter", "Recheck Cond"} {
		if condition := stringValue(values[key]); condition != "" {
			node.Condition = condition
			break
		}
	}
	if children, ok := values["Plans"].([]any); ok {
		for _, child := range children {
			if childValues, ok := child.(map[string]any); ok {
				node.Children = append(node.Children, postgresNode(childValues))
			}
		}
	}
	return node
}

// ---------- MySQL: EXPLAIN FORMAT=TREE / EXPLAIN ANALYZE ----------

// Contoh baris: "    -> Index lookup on o using idx (cid=c.id)  (cost=0.3 rows=1) (actual time=0.01..0.02 rows=1 loops=2)"
var mysqlTreeLine = regexp.MustCompile(
	`^(\s*)-> (.*?)` +
		`(?:\s+\(cost=([0-9.e+-]+)(?:\.\.([0-9.e+-]+))?\s+rows=([0-9.e+-]+)\))?` +
		`(?:\s+\(actual time=([0-9.e+-]+)\.\.([0-9.e+-]+)\s+rows=([0-9.e+-]+)\s+loops=([0-9.e+-]+)\))?` +
		`(?:\s+\(never executed\))?\s*$`)

var (
	mysqlRelation = regexp.MustCompile(` on (\S+)`)
	mysqlIndex    = regexp.MustCompile(` using (\S+)`)
)

// parseMySQLTree membaca output EXPLAIN FORMAT=TREE atau EXPLAIN ANALYZE MySQL.
// Kedalaman node ditentukan oleh indentasi (4 spasi per level).
func parseMySQLTree(text string) (*PlanNode, PlanSummary, error) {
	type frame struct {
		depth int
		node  *PlanNode
	}
	var root *PlanNode
	var stack []frame
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := mysqlTreeLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		depth := len(match[1]) / 4
		node := mysqlTreeNode(match)
		for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			if root != nil {
				// Lebih dari satu akar: bungkus agar tetap satu pohon.
				wrapper := PlanNode{NodeType: "Plan", Children: []PlanNode{*root}}
				root = &wrapper
				stack = []frame{{depth: -1, node: root}}
			} else {
				root = &node
				stack = append(stack, frame{depth: depth, node: root})
				continue
			}
		}
		parent := stack[len(stack)-1].node
		parent.Children = append(parent.Children, node)
		stack = append(stack, frame{depth: depth, node: &parent.Children[len(parent.Children)-1]})
	}
	if root == nil {
		return nil, PlanSummary{}, fmt.Errorf("mysql plan is empty")
	}
	return root, summarize(root), nil
}

func mysqlTreeNode(match []string) PlanNode {
	description := strings.TrimSpace(match[2])
	node := PlanNode{NodeType: description}
	switch {
	case strings.HasPrefix(description, "Filter: "):
		node.NodeType = "Filter"
		node.Filter = strings.TrimPrefix(description, "Filter: ")
	case strings.Contains(description, " on "):
		node.NodeType = strings.TrimSpace(description[:strings.Index(description, " on ")])
		if relation := mysqlRelation.FindStringSubmatch(description); relation != nil {
			node.Relation = strings.Trim(relation[1], "`")
		}
		if index := mysqlIndex.FindStringSubmatch(description); index != nil {
			node.Index = strings.Trim(index[1], "`")
		}
		if open := strings.Index(description, "("); open > 0 && strings.HasSuffix(description, ")") {
			node.Condition = description[open+1 : len(description)-1]
		}
	}
	if strings.Contains(strings.ToLower(node.NodeType), "join") {
		node.JoinType = node.NodeType
	}
	if match[4] != "" {
		node.StartupCost = parseNumber(match[3])
		node.TotalCost = parseNumber(match[4])
	} else {
		node.TotalCost = parseNumber(match[3])
	}
	node.PlanRows = parseNumber(match[5])
	node.ActualStartupMs = parseNumber(match[6])
	node.ActualTotalMs = parseNumber(match[7])
	node.ActualRows = parseNumber(match[8])
	node.ActualLoops = parseNumber(match[9])
	return node
}

// mysqlAccessTypes menerjemahkan kolom "type" EXPLAIN tradisional MySQL.
var mysqlAccessTypes = map[string]string{
	"ALL":             "Table scan",
	"index":           "Index scan",
	"range":           "Index range scan",
	"ref":             "Index lookup",
	"eq_ref":          "Single-row index lookup",
	"ref_or_null":     "Index lookup (or null)",
	"const":           "Constant row lookup",
	"system":          "Constant row lookup",
	"index_merge":     "Index merge",
	"fulltext":        "Fulltext index lookup",
	"unique_subquery": "Unique subquery lookup",
	"index_subquery":  "Index subquery lookup",
}

// mysqlDMLOperations memberi nama node akar untuk statement DML MySQL.
var mysqlDMLOperations = map[string]string{
	"INSERT":  "Insert into",
	"REPLACE": "Replace into",
	"UPDATE":  "Update",
	"DELETE":  "Delete from",
}

// parseMySQLTabular mengubah hasil EXPLAIN tradisional MySQL (12 kolom:
// id, select_type, table, partitions, type, possible_keys, key, key_len,
// ref, rows, filtered, Extra) menjadi pohon plan. Dipakai bila FORMAT=TREE
// tidak didukung untuk statement tersebut (mis. INSERT ... VALUES).
func parseMySQLTabular(rows [][]any) (*PlanNode, PlanSummary, error) {
	if len(rows) == 0 {
		return nil, PlanSummary{}, fmt.Errorf("mysql plan is empty")
	}
	var nodes []PlanNode
	hashJoin := false
	for _, row := range rows {
		if len(row) < 12 {
			return nil, PlanSummary{}, fmt.Errorf("unexpected mysql explain shape")
		}
		selectType := textCell(row[1])
		table := textCell(row[2])
		access := textCell(row[4])
		extra := textCell(row[11])
		node := PlanNode{NodeType: mysqlAccessTypes[access], Relation: table, Index: textCell(row[6]),
			PlanRows: parseNumber(textCell(row[9])), Filter: extra}
		if node.NodeType == "" {
			node.NodeType = selectType
		}
		if operation, ok := mysqlDMLOperations[selectType]; ok {
			node.NodeType = operation + " " + table
			if mysqlAccessTypes[access] != "" {
				node.Children = []PlanNode{{NodeType: mysqlAccessTypes[access], Relation: table, Index: node.Index, PlanRows: node.PlanRows}}
			}
		}
		if strings.Contains(extra, "hash join") {
			hashJoin = true
		}
		nodes = append(nodes, node)
	}
	root := nodes[0]
	if len(nodes) > 1 {
		joinType := "Nested loop join"
		if hashJoin {
			joinType = "Hash join"
		}
		root = PlanNode{NodeType: joinType, JoinType: joinType, Children: nodes}
	}
	return &root, summarize(&root), nil
}

// ---------- Ringkasan ----------

func summarize(root *PlanNode) PlanSummary {
	summary := PlanSummary{TotalCost: root.TotalCost, EstimatedRows: root.PlanRows, ActualRows: root.ActualRows}
	if root.ActualTotalMs != nil && root.ActualLoops != nil {
		total := *root.ActualTotalMs * *root.ActualLoops
		summary.ExecutionMs = &total
	}
	scans, indexes, joins, relations := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	var warnings []string
	var walk func(node *PlanNode)
	walk = func(node *PlanNode) {
		lower := strings.ToLower(node.NodeType)
		if strings.Contains(lower, "scan") || strings.Contains(lower, "lookup") {
			scans[node.NodeType] = true
		}
		if node.Index != "" {
			indexes[node.Index] = true
		}
		if node.JoinType != "" || strings.Contains(lower, "join") || lower == "nested loop" {
			label := node.NodeType
			if node.JoinType != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(node.JoinType)) {
				label += " (" + node.JoinType + ")"
			}
			joins[label] = true
		}
		if node.Relation != "" {
			relations[node.Relation] = true
		}
		fullScan := lower == "seq scan" || lower == "table scan"
		if fullScan && node.PlanRows != nil && *node.PlanRows >= largeSeqScanRows {
			warnings = append(warnings, fmt.Sprintf(
				"%s pada %s membaca ~%s baris tanpa index; pertimbangkan index pada kolom filter.",
				node.NodeType, node.Relation, formatCount(*node.PlanRows)))
		}
		if node.PlanRows != nil && node.ActualRows != nil {
			estimate, actual := *node.PlanRows, *node.ActualRows
			if (estimate >= 10 || actual >= 10) && (actual > estimate*10 || estimate > actual*10) {
				warnings = append(warnings, fmt.Sprintf(
					"Estimasi row %s meleset jauh (perkiraan %s, aktual %s); statistik mungkin perlu ANALYZE.",
					node.NodeType, formatCount(estimate), formatCount(actual)))
			}
		}
		for index := range node.Children {
			walk(&node.Children[index])
		}
	}
	walk(root)
	summary.ScanTypes = sortedKeys(scans)
	summary.Indexes = sortedKeys(indexes)
	summary.JoinTypes = sortedKeys(joins)
	summary.Relations = sortedKeys(relations)
	if warnings == nil {
		warnings = []string{}
	}
	summary.Warnings = warnings
	return summary
}

// prettyJSON merapikan output JSON mentah untuk tampilan "output mentah".
func prettyJSON(raw []byte) string {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buffer.String()
}

// tabularText menyusun ulang EXPLAIN tradisional MySQL sebagai teks.
func tabularText(rows [][]any) string {
	headers := []string{"id", "select_type", "table", "partitions", "type", "possible_keys", "key", "key_len", "ref", "rows", "filtered", "Extra"}
	var builder strings.Builder
	builder.WriteString(strings.Join(headers, " | "))
	for _, row := range rows {
		cells := make([]string, len(row))
		for index, cell := range row {
			cells[index] = textCell(cell)
			if cells[index] == "" {
				cells[index] = "NULL"
			}
		}
		builder.WriteString("\n" + strings.Join(cells, " | "))
	}
	return builder.String()
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func numberValue(value any) *float64 {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	return &number
}

func parseNumber(text string) *float64 {
	if text == "" {
		return nil
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	return &number
}

func textCell(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func formatCount(value float64) string {
	if value >= 1_000_000 {
		return strconv.FormatFloat(value/1_000_000, 'f', 1, 64) + " jt"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
