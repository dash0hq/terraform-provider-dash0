package converter

import "strings"

// gopkg.in/yaml.v3 always indents block sequence items one level under their
// mapping key, and offers no option to do otherwise. Terraform's yamlencode does
// the opposite, putting items flush with the key. When state holds one
// convention and the configuration holds the other, `terraform plan` marks every
// sequence line as changed, which is the noise this package exists to remove.
//
// The rewrite below is textual, because the encoder gives no hook for it. What
// keeps it honest is the caller: it only runs when the reference clearly uses
// the flush style, and the result is discarded unless it parses back to the same
// data. Anything this code gets wrong costs an indented diff, never a wrong
// document.

// referenceUsesFlushSequences reports whether reference puts block sequence
// items flush with their mapping key. It answers false when reference indents
// them, and when it holds no block sequence to learn from.
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

// flushSequences moves every block sequence the encoder indented under a mapping
// key back to that key's own indentation, along with everything nested inside
// it. It reports false when a shift would run past the start of a line.
func flushSequences(encoded string) (string, bool) {
	lines := strings.Split(encoded, "\n")
	out := make([]string, 0, len(lines))

	// Indents of the enclosing sequences that a mapping key introduced. Only
	// those are the ones the encoder pushed a level deeper than flush style.
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

		// A sequence at the document root already matches flush style, so only
		// one hanging off a key at the level above gets moved.
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

// keyWithNoValue reports whether a line is a mapping key whose value follows on
// later lines, which is the only shape a block sequence can hang from, and where
// that value starts.
//
// A key can also open on a sequence item's own line, as in `- actions:`. The
// item's later keys sit where the text after the dash begins, so that is the
// indentation a sequence hanging off this key is measured against.
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
