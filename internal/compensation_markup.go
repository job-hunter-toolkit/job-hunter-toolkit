package internal

import (
	"html"
	"strings"

	xhtml "golang.org/x/net/html"
)

// payRangeClass is the class Greenhouse gives the container it renders when an
// employer fills in its structured pay-range field.
//
// Finding pay inside this container is categorically different from finding it in
// prose: the container itself declares that its contents are the pay range for
// this job, so no guessing about cue words is involved. Measured presence on
// Greenhouse boards that use the field: 421 of 800 Databricks postings, 90 of 119
// Robinhood postings.
const payRangeClass = "pay-range"

// ParseCompensationFromDescription extracts a pay range from a job description,
// preferring structure over prose.
//
// It tries sources in descending order of trustworthiness and stops at the first
// that yields a range:
//
//  1. a structured pay-range container the board rendered from a real field,
//     reported as [ProvenanceStructured];
//  2. description prose, reported as [ProvenanceDescription].
//
// A platform that exposes pay through its API should be read there instead and
// reported as [ProvenanceEmployer]; this function is for the platforms that do
// not. Returns nil when no source yields a confident answer.
//
// The layering is the point: it means a wrong number is far more likely to come
// from the clearly-marked prose path than from a structural one, and callers can
// act on that distinction rather than trusting everything equally.
func ParseCompensationFromDescription(description string) *Compensation {
	if description == "" {
		return nil
	}

	if comp := parsePayRangeMarkup(description); comp != nil {
		return comp
	}

	return ParseCompensationFromText(description)
}

// parsePayRangeMarkup extracts pay from a structured pay-range container.
//
// The description is parsed as HTML rather than scanned with a regular
// expression, so nesting and attribute order do not matter. Entities are decoded
// first because some boards publish their markup entity-encoded, which would
// otherwise hide the container entirely.
func parsePayRangeMarkup(description string) *Compensation {
	decoded := html.UnescapeString(description)

	// Only pay for a full HTML parse when the marker is actually present.
	if !strings.Contains(decoded, payRangeClass) {
		return nil
	}

	doc, err := xhtml.Parse(strings.NewReader(decoded))
	if err != nil {
		return nil
	}

	container := findPayRangeContainer(doc)
	if container == nil {
		return nil
	}

	text := normalizeText(nodeText(container))
	if text == "" {
		return nil
	}

	// No cue check here: the container has already established that these figures
	// are the pay for this job. Plausibility is still enforced, because a
	// malformed value should not become a nonsense salary.
	comp := parseMoneyRangeIn(text)
	if comp == nil {
		return nil
	}

	comp.Provenance = ProvenanceStructured

	return comp
}

// findPayRangeContainer returns the first element whose class marks it as a pay
// range container.
func findPayRangeContainer(node *xhtml.Node) *xhtml.Node {
	if node.Type == xhtml.ElementNode && hasClass(node, payRangeClass) {
		return node
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findPayRangeContainer(child); found != nil {
			return found
		}
	}

	return nil
}

// hasClass reports whether an element carries the given class, matching whole
// class tokens so "pay-range-note" does not match "pay-range".
func hasClass(node *xhtml.Node, class string) bool {
	for _, attr := range node.Attr {
		if attr.Key != "class" {
			continue
		}

		for _, token := range strings.Fields(attr.Val) {
			if strings.EqualFold(token, class) {
				return true
			}
		}
	}

	return false
}

// nodeText returns the concatenated text of a node and its descendants,
// separating each text run with a space so adjacent spans do not merge into one
// unparseable number.
func nodeText(node *xhtml.Node) string {
	var builder strings.Builder

	var walk func(*xhtml.Node)

	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			builder.WriteString(n.Data)
			builder.WriteString(" ")
		}

		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)

	return builder.String()
}

// parseMoneyRangeIn extracts a range, or failing that a single figure, from text
// already known to describe pay. It enforces plausibility but not cue proximity.
func parseMoneyRangeIn(text string) *Compensation {
	lowered := strings.ToLower(text)

	if match := moneyRangePattern.FindStringSubmatchIndex(text); match != nil {
		low := parseMoney(text[match[2]:match[3]], group(text, match, 4))
		high := parseMoney(text[match[6]:match[7]], group(text, match, 8))

		if comp := buildCompensation(low, high, lowered, match[1]); comp != nil {
			return comp
		}
	}

	if match := moneySinglePattern.FindStringSubmatchIndex(text); match != nil {
		value := parseMoney(text[match[2]:match[3]], group(text, match, 4))

		if comp := buildCompensation(value, 0, lowered, match[1]); comp != nil {
			return comp
		}
	}

	return nil
}
