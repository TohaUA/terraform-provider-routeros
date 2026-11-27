package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type RawAttr struct {
	Key      string
	RawValue string
}

type ExportCommand struct {
	Path     string
	Action   string
	Target   string
	FindArgs []RawAttr
	Attrs    []RawAttr
}

func ParseExport(config string) []ExportCommand {
	normalized := strings.ReplaceAll(config, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	var cmds []ExportCommand
	for _, line := range lines {
		if cmd, ok := parseExportLine(line); ok {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func parseExportLine(line string) (ExportCommand, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "/") {
		return ExportCommand{}, false
	}

	var pathParts []string
	var action string
	i := 0
	for i < len(line) {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			break
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		tok := line[start:i]
		if tok == "add" || tok == "set" {
			action = tok
			break
		}
		pathParts = append(pathParts, tok)
	}

	if action == "" || len(pathParts) == 0 {
		return ExportCommand{}, false
	}

	cmd := ExportCommand{
		Path:   strings.Join(pathParts, " "),
		Action: action,
	}

	rest := strings.TrimLeft(line[i:], " \t")
	if action == "set" && len(rest) > 0 {
		if rest[0] == '[' {
			endIdx := findClosingBracket(rest)
			if endIdx > 0 {
				inner := strings.TrimSpace(rest[1:endIdx])
				if inner == "find" {
					inner = ""
				} else if strings.HasPrefix(inner, "find") && (len(inner) == 4 || inner[4] == ' ' || inner[4] == '\t') {
					inner = strings.TrimLeft(inner[4:], " \t")
				}
				cmd.FindArgs = scanAttributePairs(inner)
				rest = strings.TrimLeft(rest[endIdx+1:], " \t")
			}
		} else {
			tok, remainder, hasUnquotedEq := readNextToken(rest)
			if tok != "" && !hasUnquotedEq {
				cmd.Target = unescapeRouterOSValue(tok)
				rest = strings.TrimLeft(remainder, " \t")
			}
		}
	}

	cmd.Attrs = scanAttributePairs(rest)
	return cmd, true
}

func findClosingBracket(s string) int {
	inQuotes := false
	depth := 0
	for i := 0; i < len(s); i++ {
		if inQuotes {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				inQuotes = false
			}
			continue
		}
		switch s[i] {
		case '"':
			inQuotes = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func readNextToken(s string) (token, rest string, hasUnquotedEq bool) {
	i := 0
	inQuotes := false
	for i < len(s) {
		if inQuotes {
			if s[i] == '\\' {
				if i+1 < len(s) {
					i += 2
				} else {
					i++
				}
				continue
			}
			if s[i] == '"' {
				inQuotes = false
			}
			i++
			continue
		}
		if s[i] == '"' {
			inQuotes = true
			i++
			continue
		}
		if s[i] == '=' {
			hasUnquotedEq = true
		}
		if s[i] == ' ' || s[i] == '\t' {
			break
		}
		i++
	}
	return s[:i], s[i:], hasUnquotedEq
}

func scanAttributePairs(s string) []RawAttr {
	var attrs []RawAttr
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}

		keyStart := i
		foundEq := false
		for i < len(s) && s[i] != ' ' && s[i] != '\t' {
			if s[i] == '=' {
				foundEq = true
				break
			}
			i++
		}
		if !foundEq {
			continue
		}

		key := s[keyStart:i]
		i++ // consume '='

		valStart := i
		inQuotes := false
		braceDepth := 0
		isBraceList := i < len(s) && s[i] == '{'
		for i < len(s) {
			if inQuotes {
				if s[i] == '\\' {
					if i+1 < len(s) {
						i += 2
					} else {
						i++
					}
					continue
				}
				if s[i] == '"' {
					inQuotes = false
				}
				i++
				continue
			}

			if s[i] == '"' {
				inQuotes = true
				i++
				continue
			}
			if isBraceList && s[i] == '{' {
				braceDepth++
				i++
				continue
			}
			if isBraceList && s[i] == '}' && braceDepth > 0 {
				braceDepth--
				i++
				continue
			}
			if (s[i] == ' ' || s[i] == '\t') && braceDepth == 0 {
				break
			}
			i++
		}

		attrs = append(attrs, RawAttr{
			Key:      key,
			RawValue: s[valStart:i],
		})
	}
	return attrs
}

func unescapeRouterOSValue(raw string) string {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return raw
	}
	inner := raw[1 : len(raw)-1]
	out := make([]byte, 0, len(inner))
	for i := 0; i < len(inner); {
		if inner[i] != '\\' {
			out = append(out, inner[i])
			i++
			continue
		}
		if i+1 >= len(inner) {
			out = append(out, '\\')
			i++
			continue
		}
		switch inner[i+1] {
		case '\\':
			out = append(out, '\\')
			i += 2
		case '"':
			out = append(out, '"')
			i += 2
		case 'n':
			out = append(out, '\n')
			i += 2
		case 'r':
			out = append(out, '\r')
			i += 2
		case 't':
			out = append(out, '\t')
			i += 2
		case 'a':
			out = append(out, '\a')
			i += 2
		case 'b':
			out = append(out, '\b')
			i += 2
		case 'f':
			out = append(out, '\f')
			i += 2
		case 'v':
			out = append(out, '\v')
			i += 2
		case '$':
			out = append(out, '$')
			i += 2
		case '?':
			out = append(out, '?')
			i += 2
		case '_':
			out = append(out, ' ')
			i += 2
		default:
			if i+2 < len(inner) && isHexDigit(inner[i+1]) && isHexDigit(inner[i+2]) {
				b := (hexVal(inner[i+1]) << 4) | hexVal(inner[i+2])
				out = append(out, b)
				i += 3
			} else {
				out = append(out, '\\', inner[i+1])
				i += 2
			}
		}
	}
	return string(out)
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexVal(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}

func quoteRouterOSValue(s string) string {
	if s == "" {
		return ""
	}
	needsQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= ' ' || c == '"' || c == '\\' || c == '$' || c == '?' || c == '=' || c == '[' || c == ']' || c == '{' || c == '}' || c == ';' || c == ',' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '$':
			b.WriteString(`\$`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func splitRouterOSList(raw string) []string {
	if raw == "" || raw == `""` {
		return nil
	}

	if len(raw) >= 2 && raw[0] == '{' && raw[len(raw)-1] == '}' {
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		if inner == "" {
			return nil
		}
		sep := byte(';')
		if !hasUnquotedByte(inner, ';') && hasUnquotedByte(inner, ',') {
			sep = ','
		}
		parts := splitUnquoted(inner, sep)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			out = append(out, unescapeRouterOSValue(p))
		}
		return out
	}

	parts := splitUnquoted(raw, ',')
	if len(parts) > 1 {
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			out = append(out, unescapeRouterOSValue(p))
		}
		return out
	}

	unescaped := unescapeRouterOSValue(raw)
	if unescaped == "" {
		return nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' && strings.Contains(unescaped, ",") {
		return strings.Split(unescaped, ",")
	}
	return []string{unescaped}
}

func hasUnquotedByte(s string, target byte) bool {
	inQuotes := false
	for i := 0; i < len(s); i++ {
		if inQuotes {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				inQuotes = false
			}
			continue
		}
		if s[i] == '"' {
			inQuotes = true
			continue
		}
		if s[i] == target {
			return true
		}
	}
	return false
}

func splitUnquoted(s string, sep byte) []string {
	var parts []string
	inQuotes := false
	start := 0
	for i := 0; i < len(s); i++ {
		if inQuotes {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				inQuotes = false
			}
			continue
		}
		if s[i] == '"' {
			inQuotes = true
			continue
		}
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func formatHCLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
			i += size
			continue
		}
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '$', '%':
			b.WriteRune(r)
			if i+size < len(s) && s[i+size] == '{' {
				b.WriteRune(r)
			}
		default:
			if !unicode.IsPrint(r) {
				if r < 0x10000 {
					fmt.Fprintf(&b, `\u%04x`, r)
				} else {
					fmt.Fprintf(&b, `\U%08x`, r)
				}
			} else {
				b.WriteRune(r)
			}
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}
