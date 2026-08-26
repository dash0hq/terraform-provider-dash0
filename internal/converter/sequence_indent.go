package converter

import "strings"

// yaml.v3 always indents block sequence items under their mapping key, with no
// option not to; Terraform's yamlencode puts them flush. Mixing the two makes
// every sequence line read as changed.
//
// The rewrite is textual because the encoder exposes no hook. The caller runs it
// only for a flush reference and discards the result unless it parses back to
// the same data, so a mistake costs an indented diff, not a wrong document.

// Returns false for a reference with no block sequence to learn from.
func referenceUsesFlushSequences(reference string) bool {
	lines := strings.Split(reference, "\n")
	for i, line := range lines {
		keyIndent, isKey := keyWithNoValue(line)
		if !isKey {
			continue
		}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			indent, content := splitIndent(next)
			if !isSequenceItem(content) {
				break
			}
			if indent == keyIndent {
				return true
			}
			break
		}
	}
	return false
}

// Moves each sequence the encoder indented under a key back to that key's
// indentation, with everything nested inside it.
func flushSequences(encoded string) (string, bool) {
	lines := strings.Split(encoded, "\n")
	out := make([]string, 0, len(lines))

	// Enclosing sequences a key introduced, the only ones pushed a level deep.
	var open []int
	previousKeyIndent := -1

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, line)
			continue
		}

		indent, content := splitIndent(line)
		item := isSequenceItem(content)

		for len(open) > 0 {
			top := open[len(open)-1]
			if indent < top || (indent == top && !item) {
				open = open[:len(open)-1]
				continue
			}
			break
		}

		// A root sequence already matches, so only one hanging off a key moves.
		if item && (len(open) == 0 || indent > open[len(open)-1]) &&
			previousKeyIndent >= 0 && indent == previousKeyIndent+encoderIndent {
			open = append(open, indent)
		}

		shift := len(open) * encoderIndent
		if shift > indent {
			return "", false
		}
		out = append(out, line[shift:])

		previousKeyIndent = -1
		if keyIndent, isKey := keyWithNoValue(line); isKey {
			previousKeyIndent = keyIndent
		}
	}

	return strings.Join(out, "\n"), true
}

func splitIndent(line string) (indent int, content string) {
	content = strings.TrimLeft(line, " ")
	return len(line) - len(content), content
}

func isSequenceItem(content string) bool {
	return content == "-" || strings.HasPrefix(content, "- ")
}

// Reports whether a line is a key whose value follows, and where it starts. A
// key can open on an item's line, `- actions:`, where later keys sit after the
// dash.
func keyWithNoValue(line string) (indent int, ok bool) {
	indent, content := splitIndent(line)
	for strings.HasPrefix(content, "- ") {
		indent += len("- ")
		content = content[len("- "):]
	}
	if content == "" || content == "-" {
		return 0, false
	}
	return indent, strings.HasSuffix(content, ":")
}
