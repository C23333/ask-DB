package api

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

func buildTableDocument(title string, notes []string, columns []string, rows [][]string) string {
	var builder strings.Builder
	builder.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><style>
body{font-family:"Microsoft YaHei",Arial,sans-serif;margin:24px;color:#222;}
h2{margin-bottom:4px;}
.meta{margin:2px 0;color:#555;font-size:13px;}
table{border-collapse:collapse;width:100%;margin-top:12px;}
th,td{border:1px solid #d0d7de;padding:8px;font-size:13px;vertical-align:top;}
th{background-color:#f6f8fa;text-align:left;}
tbody tr:nth-child(even){background-color:#fbfbfb;}
.empty{color:#888;text-align:center;font-style:italic;}
</style></head><body>`)
	if title != "" {
		builder.WriteString(fmt.Sprintf("<h2>%s</h2>", html.EscapeString(title)))
	}
	for _, note := range notes {
		if strings.TrimSpace(note) == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("<p class=\"meta\">%s</p>", html.EscapeString(note)))
	}
	builder.WriteString("<table><thead><tr>")
	for _, col := range columns {
		builder.WriteString(fmt.Sprintf("<th>%s</th>", html.EscapeString(col)))
	}
	builder.WriteString("</tr></thead><tbody>")
	if len(rows) == 0 {
		colspan := strconv.Itoa(max(1, len(columns)))
		builder.WriteString(fmt.Sprintf("<tr><td class=\"empty\" colspan=\"%s\">暂无数据</td></tr>", colspan))
	} else {
		for _, row := range rows {
			builder.WriteString("<tr>")
			for _, cell := range row {
				builder.WriteString("<td>")
				builder.WriteString(escapeCell(cell))
				builder.WriteString("</td>")
			}
			builder.WriteString("</tr>")
		}
	}
	builder.WriteString("</tbody></table></body></html>")
	return builder.String()
}

func escapeCell(value string) string {
	if value == "" {
		return ""
	}
	escaped := html.EscapeString(value)
	return strings.ReplaceAll(escaped, "\n", "<br/>")
}

func stringifySQLRows(rows [][]interface{}) [][]string {
	out := make([][]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(row))
		for j, val := range row {
			cells[j] = formatCellValue(val)
		}
		out[i] = cells
	}
	return out
}

func formatCellValue(val interface{}) string {
	switch v := val.(type) {
	case nil:
		return ""
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	case *time.Time:
		if v == nil {
			return ""
		}
		return v.Format("2006-01-02 15:04:05")
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
