package corpus

import (
	"fmt"
	"strings"

	"github.com/cupogo/andvari/models/oid"
	"github.com/cupogo/andvari/utils/array"
	"github.com/liut/morign/pkg/utils/words"
)

// GetSubject returns the document subject (title + heading)
func (z *Document) GetSubject() string {
	return fmt.Sprintf("%s %s", z.Title, z.Heading)
}

func (z *Document) GetFullText() string {
	return fmt.Sprintf("%s\n%s\n%s", z.Title, z.Heading, z.Content)
}

// IDs returns all document IDs in the document list
func (z Documents) IDs() (out oid.OIDs) {
	for _, doc := range z {
		out = append(out, doc.ID)
	}
	return
}

// MarkdownText converts document list to Markdown format text for LLM context
func (z Documents) MarkdownText() string {
	if len(z) == 0 {
		return "No relevant information found in the knowledge base."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d relevant documents in the knowledge base:\n\n", len(z))
	for _, doc := range z {
		sb.WriteString("---\nID: ")
		sb.WriteString(doc.StringID())
		sb.WriteString("\n\n## ")
		sb.WriteString(doc.Title)
		sb.WriteString("\n\n### ")
		sb.WriteString(doc.Heading)
		sb.WriteString("\n\n")
		sb.WriteString(doc.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Headings returns all document headings in the document list
func (z Documents) Headings() []string {
	headings := make([]string, len(z))
	for i, doc := range z {
		headings[i] = doc.Heading
	}
	return headings
}

// DocumentIDs returns deduplicated document IDs from the document match list
func (z DocMatches) DocumentIDs() (out oid.OIDs) {
	m := make(map[oid.OID]array.Empty)
	for _, p := range z {
		m[p.DocID] = array.Empty{}
	}
	for k := range m {
		out = append(out, k)
	}
	return
}

// Subjects returns all document subjects from the document match list
func (z DocMatches) Subjects(n int) (out []string) {
	out = make([]string, len(z))
	for i := range z {
		if n <= 0 {
			out[i] = z[i].Subject
		} else {
			out[i] = words.TakeHead(z[i].Subject, n, "..")
		}

	}
	return
}
