package capability

import (
	"strings"

	"github.com/liut/morign/pkg/models/aigc"
)

// CapabilityMatch 能力匹配结果 - 复用 MatchResult
type CapabilityMatch = aigc.MatchResult

// GetSubject returns the capability subject (tags + summary + description)
func (z *CapabilityBasic) GetSubject() string {
	var parts []string
	if len(z.Tags) > 0 {
		parts = append(parts, z.Tags[0])
	}
	if z.Summary != "" {
		parts = append(parts, z.Summary)
	}
	return strings.Join(parts, " ")
}

// GetSubjectWithParams returns subject enhanced with parameters info
func (z *CapabilityBasic) GetSubjectWithParams() string {
	subject := z.GetSubject()
	// Future: parse parameters and responses to extract key fields
	return subject
}

func (z Capabilities) Endpoints() []string {
	out := make([]string, len(z))
	for i := range z {
		out[i] = z[i].Endpoint
	}
	return out
}

// FilterParams removes parameters with the specified name from the list
func FilterParams(params []SwaggerParam, excludeName string) []SwaggerParam {
	if len(params) == 0 {
		return params
	}
	filtered := make([]SwaggerParam, 0, len(params))
	for _, p := range params {
		if p.Name != excludeName {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// EnrichSortableFields extracts <sortable>field1,field2...</sortable> from Description
// and appends the sortable fields info to Parameters[sort].Description if sort param exists
func (z *CapabilityBasic) EnrichSortableFields() {
	const sortablePrefix = "<sortable>"
	const sortableSuffix = "</sortable>"

	start := strings.Index(z.Description, sortablePrefix)
	if start == -1 {
		return
	}

	end := strings.Index(z.Description[start:], sortableSuffix)
	if end == -1 {
		return
	}
	end += start + len(sortableSuffix)

	sortableContent := z.Description[start+len(sortablePrefix) : end-len(sortableSuffix)]
	sortableFields := strings.Split(sortableContent, ",")

	var sortParam *SwaggerParam
	for i := range z.Parameters {
		if z.Parameters[i].Name == "sort" {
			sortParam = &z.Parameters[i]
			break
		}
	}
	if sortParam == nil {
		return
	}

	existingDesc := sortParam.Description
	if existingDesc != "" && !strings.HasSuffix(existingDesc, " ") {
		sortParam.Description += " "
	}
	sortParam.Description += "Sortable fields: " + strings.Join(sortableFields, ", ")

	z.Description = z.Description[:start] + z.Description[end:]
}
