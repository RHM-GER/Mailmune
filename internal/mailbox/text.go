package mailbox

import "strings"

const htmlToTextLimit = 256 << 10

// HTMLToText converts HTML to plain text deterministically and fully
// offline. It never resolves, fetches or follows URLs. The conversion is
// intentionally simple and conservative: script/style content is dropped,
// block boundaries become line breaks, entities are decoded and whitespace
// is collapsed.
func HTMLToText(input string) string {
	if len(input) > htmlToTextLimit {
		input = input[:htmlToTextLimit]
	}
	input = dropBlocks(input, "script")
	input = dropBlocks(input, "style")

	var builder strings.Builder
	builder.Grow(len(input))
	for i := 0; i < len(input); {
		character := input[i]
		switch {
		case character == '<':
			tag, rest, ok := readTag(input[i:])
			if !ok {
				// Malformed tag: treat '<' as text.
				builder.WriteByte(character)
				i++
				continue
			}
			if isBlockBoundary(tag) {
				builder.WriteByte('\n')
			}
			i += len(input[i:]) - len(rest)
		case character == '&':
			entity, rest, ok := readEntity(input[i:])
			if ok {
				builder.WriteString(entity)
				i += len(input[i:]) - len(rest)
				continue
			}
			builder.WriteByte(character)
			i++
		default:
			builder.WriteByte(character)
			i++
		}
	}
	return normalizeWhitespace(builder.String())
}

// dropBlocks removes <tag ...>...</tag> regions case-insensitively.
func dropBlocks(input, tag string) string {
	lower := strings.ToLower(input)
	open := "<" + tag
	close := "</" + tag
	var builder strings.Builder
	rest := input
	lowerRest := lower
	for {
		start := strings.Index(lowerRest, open)
		if start < 0 {
			builder.WriteString(rest)
			return builder.String()
		}
		// Ensure the opener is a real tag: followed by space, '>' or '/'.
		after := start + len(open)
		if after >= len(rest) || (rest[after] != '>' && rest[after] != ' ' && rest[after] != '/' && rest[after] != '\t' && rest[after] != '\n' && rest[after] != '\r') {
			builder.WriteString(rest[:after])
			rest = rest[after:]
			lowerRest = lowerRest[after:]
			continue
		}
		end := strings.Index(lowerRest[after:], close)
		builder.WriteString(rest[:start])
		if end < 0 {
			// Unterminated block: drop the remainder.
			return builder.String()
		}
		closeEnd := after + end
		if gt := strings.IndexByte(rest[closeEnd:], '>'); gt >= 0 {
			closeEnd += gt + 1
		} else {
			closeEnd = len(rest)
		}
		rest = rest[closeEnd:]
		lowerRest = lowerRest[closeEnd:]
	}
}

// readTag consumes a '<...>' token and returns the tag name and the rest of
// the input after the tag.
func readTag(input string) (name string, rest string, ok bool) {
	if len(input) == 0 || input[0] != '<' {
		return "", input, false
	}
	end := strings.IndexByte(input, '>')
	if end < 0 {
		return "", input, false
	}
	body := input[1:end]
	body = strings.TrimLeft(body, "/ \t\r\n")
	nameEnd := strings.IndexFunc(body, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '/' || r == '>'
	})
	if nameEnd >= 0 {
		body = body[:nameEnd]
	}
	return strings.ToLower(body), input[end+1:], true
}

func isBlockBoundary(tag string) bool {
	switch tag {
	case "br", "/br", "p", "/p", "div", "/div", "tr", "/tr", "li", "/li",
		"table", "/table", "h1", "/h1", "h2", "/h2", "h3", "/h3", "h4", "/h4",
		"h5", "/h5", "h6", "/h6", "blockquote", "/blockquote", "hr", "/hr",
		"head", "/head", "style", "/style", "script", "/script":
		return true
	}
	return false
}

func readEntity(input string) (decoded string, rest string, ok bool) {
	semicolon := strings.IndexByte(input, ';')
	if semicolon < 0 || semicolon > 10 {
		return "", input, false
	}
	name := input[1:semicolon]
	rest = input[semicolon+1:]
	switch strings.ToLower(name) {
	case "amp":
		return "&", rest, true
	case "lt":
		return "<", rest, true
	case "gt":
		return ">", rest, true
	case "quot":
		return "\"", rest, true
	case "apos", "#39":
		return "'", rest, true
	case "nbsp":
		return " ", rest, true
	default:
		if value, parsed := decodeNumericEntity(name); parsed {
			return value, rest, true
		}
		return "", input, false
	}
}

func decodeNumericEntity(name string) (string, bool) {
	lower := strings.ToLower(name)
	var codePoint rune
	if strings.HasPrefix(lower, "#x") {
		var value int
		for _, character := range lower[2:] {
			switch {
			case character >= '0' && character <= '9':
				value = value*16 + int(character-'0')
			case character >= 'a' && character <= 'f':
				value = value*16 + int(character-'a') + 10
			default:
				return "", false
			}
			if value > 0x10FFFF {
				return "", false
			}
		}
		codePoint = rune(value)
	} else if strings.HasPrefix(lower, "#") {
		value := 0
		for _, character := range lower[1:] {
			if character < '0' || character > '9' {
				return "", false
			}
			value = value*10 + int(character-'0')
			if value > 0x10FFFF {
				return "", false
			}
		}
		codePoint = rune(value)
	} else {
		return "", false
	}
	if codePoint == 0 {
		return "", false
	}
	return string(codePoint), true
}

func normalizeWhitespace(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	spaceRun := false
	newlineRun := 0
	for _, character := range input {
		switch character {
		case '\n', '\r':
			if newlineRun < 2 {
				builder.WriteByte('\n')
			}
			newlineRun++
			spaceRun = false
		case ' ', '\t':
			spaceRun = true
		default:
			if spaceRun && newlineRun == 0 && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			newlineRun = 0
			spaceRun = false
			builder.WriteRune(character)
		}
	}
	return strings.TrimSpace(builder.String())
}
