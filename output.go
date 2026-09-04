package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// printData writes one API `data` payload: pretty JSON when piped (the
// agent case — unambiguous, parseable, diffable), a table on a terminal.
func printData(w io.Writer, data json.RawMessage, format string) error {
	if format == "table" {
		return printTable(w, data)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return fmt.Errorf("the server sent JSON that does not re-encode: %w", err)
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// pair and orderedObject keep a JSON object's key order through decoding.
// The serializer emits id first and created_at/updated_at last, and a table
// should show columns in that order — a map would shuffle them.
type pair struct {
	key   string
	value any
}

type orderedObject []pair

func (o orderedObject) get(key string) (any, bool) {
	for _, p := range o {
		if p.key == key {
			return p.value, true
		}
	}
	return nil, false
}

func (o orderedObject) keys() []string {
	keys := make([]string, 0, len(o))
	for _, p := range o {
		keys = append(keys, p.key)
	}
	return keys
}

// decodeOrdered decodes JSON preserving object key order, for table output.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return decodeValue(dec)
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok := tok.(type) {
	case json.Delim:
		switch tok {
		case '{':
			obj := orderedObject{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return nil, err
				}
				value, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj = append(obj, pair{key.(string), value})
			}
			_, err := dec.Token() // closing '}'
			return obj, err
		case '[':
			arr := []any{}
			for dec.More() {
				value, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, value)
			}
			_, err := dec.Token() // closing ']'
			return arr, err
		}
		return nil, fmt.Errorf("unexpected delimiter %q", tok)
	default:
		return tok, nil
	}
}

// printTable renders a list of records as columns, or a single record as a
// key/value listing. A *_cents column prints as a decimal amount — nobody
// reads 120050 as twelve hundred dollars at a glance — while the JSON view
// stays in cents, the API's contract.
func printTable(w io.Writer, data json.RawMessage) error {
	value, err := decodeOrdered(data)
	if err != nil {
		return fmt.Errorf("the server sent JSON that does not decode: %w", err)
	}

	switch value := value.(type) {
	case []any:
		if len(value) == 0 {
			fmt.Fprintln(w, "(no records)")
			return nil
		}
		rows := make([]orderedObject, 0, len(value))
		for _, item := range value {
			obj, ok := item.(orderedObject)
			if !ok {
				fmt.Fprintln(w, formatScalar(item))
				continue
			}
			rows = append(rows, obj)
		}
		return printRows(w, rows)
	case orderedObject:
		for _, p := range value {
			fmt.Fprintf(w, "%-18s %s\n", p.key+":", formatCell(p.key, p.value))
		}
		return nil
	default:
		fmt.Fprintln(w, formatScalar(value))
		return nil
	}
}

func printRows(w io.Writer, rows []orderedObject) error {
	columns := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for _, key := range row.keys() {
			if !seen[key] {
				seen[key] = true
				columns = append(columns, key)
			}
		}
	}

	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		line := make([]string, 0, len(columns))
		for _, column := range columns {
			value, _ := row.get(column)
			line = append(line, formatCell(column, value))
		}
		cells = append(cells, line)
	}

	// Widths count characters, not bytes: a name like “José” is 5 bytes and
	// 4 columns wide, and padding by length would push every cell to its
	// right out of line — which is most of them, given the typographic
	// apostrophes the product's copy uses.
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = utf8.RuneCountInString(column)
	}
	for _, line := range cells {
		for i, cell := range line {
			if width := utf8.RuneCountInString(cell); width > widths[i] {
				widths[i] = width
			}
		}
	}

	var b strings.Builder
	writeRow := func(line []string) {
		for i, cell := range line {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(cell)
			if i < len(line)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)))
			}
		}
		b.WriteByte('\n')
	}
	writeRow(columns)
	for _, line := range cells {
		writeRow(line)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// formatCell renders one value for a table cell; the column name is what
// lets money stop being cents.
func formatCell(column string, value any) string {
	if value == nil {
		return ""
	}
	if strings.HasSuffix(column, "_cents") {
		if n, ok := toInt64(value); ok {
			return fmt.Sprintf("%.2f", float64(n)/100)
		}
	}
	return formatScalar(value)
}

func formatScalar(value any) string {
	switch value := value.(type) {
	case json.Number:
		return value.String()
	case bool:
		return fmt.Sprintf("%t", value)
	case string:
		return value
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprintf("%v", value)
		}
		return string(encoded)
	}
}

func toInt64(value any) (int64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	if err != nil {
		return 0, false
	}
	return i, true
}
