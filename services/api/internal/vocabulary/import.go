package vocabulary

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

type Entry struct {
	Word     string `json:"word"`
	Phonetic string `json:"phonetic"`
	POS      string `json:"pos"`
	Chinese  string `json:"chinese"`
	Row      int    `json:"row"`
}
type Issue struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}
type Preview struct {
	Entries    []Entry `json:"entries"`
	Errors     []Issue `json:"errors"`
	Duplicates int     `json:"duplicates"`
}

var plainRow = regexp.MustCompile(`^(.+?)\s+(/[^/]+/|\[[^\]]+\])\s+(\S+)\s+(.+)$`)
var english = regexp.MustCompile(`^[a-zA-Z][a-zA-Z '\x{2019}.()-]*$`)

func Parse(text string) Preview {
	result := Preview{Entries: []Entry{}, Errors: []Issue{}}
	text = strings.TrimPrefix(text, "\ufeff")
	first := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			first = line
			break
		}
	}
	rows := [][]string{}
	lineNumbers := []int{}
	if plainRow.FindStringSubmatch(strings.TrimSpace(first)) == nil && (strings.Contains(first, "\t") || strings.Contains(first, ",")) {
		reader := csv.NewReader(strings.NewReader(text))
		reader.FieldsPerRecord = -1
		reader.TrimLeadingSpace = true
		if strings.Contains(first, "\t") {
			reader.Comma = '\t'
		}
		for {
			before := reader.InputOffset()
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				result.Errors = append(result.Errors, Issue{len(rows) + 1, "CSV/TSV 格式有误：检查引号与列数"})
				return result
			}
			line := strings.Count(text[:before], "\n") + 1
			rows = append(rows, row)
			lineNumbers = append(lineNumbers, line)
		}
	} else {
		for index, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if m := plainRow.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				rows = append(rows, m[1:])
				lineNumbers = append(lineNumbers, index+1)
			} else {
				rows = append(rows, []string{line})
				lineNumbers = append(lineNumbers, index+1)
			}
		}
	}
	seen := map[string]string{}
	for i, row := range rows {
		line := lineNumbers[i]
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		if len(row) == 4 && len(result.Entries) == 0 {
			a := strings.ToLower(strings.TrimSpace(row[0]))
			if a == "word" || a == "单词" {
				continue
			}
		}
		if len(row) != 4 {
			result.Errors = append(result.Errors, Issue{line, "需要四列：单词、音标、词性、中文"})
			continue
		}
		for j := range row {
			row[j] = strings.TrimSpace(row[j])
		}
		entry := Entry{strings.ToLower(strings.Join(strings.Fields(row[0]), " ")), row[1], row[2], row[3], line}
		valid := english.MatchString(entry.Word) && len([]rune(entry.Word)) <= 100 && entry.Phonetic != "" && len([]rune(entry.Phonetic)) <= 150 && entry.POS != "" && len(entry.POS) <= 40 && entry.Chinese != "" && len([]rune(entry.Chinese)) <= 500
		for _, field := range row {
			if strings.ContainsFunc(field, func(r rune) bool { return unicode.IsControl(r) }) {
				valid = false
			}
		}
		if !valid {
			result.Errors = append(result.Errors, Issue{line, "字段为空、过长或包含不支持的字符"})
			continue
		}
		key := entry.Word + "\x00" + entry.POS + "\x00" + entry.Chinese
		if old, exists := seen[key]; exists {
			if old != entry.Phonetic {
				result.Errors = append(result.Errors, Issue{line, "同一义项的重复行音标不一致"})
				continue
			}
			result.Duplicates++
			continue
		}
		seen[key] = entry.Phonetic
		result.Entries = append(result.Entries, entry)
		if len(result.Entries) > 10000 {
			result.Errors = append(result.Errors, Issue{line, "一次最多导入 10000 条，请分批导入"})
			break
		}
	}
	if len(result.Entries) == 0 && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, Issue{0, "没有可导入的词条"})
	}
	return result
}
func (p Preview) Valid() error {
	if len(p.Errors) > 0 {
		return fmt.Errorf("import has %d errors", len(p.Errors))
	}
	return nil
}
