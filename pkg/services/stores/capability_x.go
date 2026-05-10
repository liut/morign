package stores

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cupogo/andvari/models/oid"
	"github.com/liut/morign/pkg/models/capability"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/settings"
)

// CapabilityStoreX is the capability storage extension interface
type CapabilityStoreX interface {
	CountCapability(ctx context.Context) (int, error)
	GetCapabilityWith(ctx context.Context, method, endpoint string) (*capability.Capability, error)
	ImportCapabilities(ctx context.Context, r io.Reader, lw io.Writer, markMissingPrefix string) error
	CleanupMissedCapabilities(ctx context.Context, lw io.Writer, prefix string, dryRun bool) error
	SyncEmbeddingCapabilities(ctx context.Context, spec *CapCapabilitySpec) error
	MatchCapabilities(ctx context.Context, ms MatchSpec) (data capability.Capabilities, err error)
	MatchVectorWith(ctx context.Context, vec corpus.Vector, threshold float32, limit int) (data []capability.CapabilityMatch, err error)
	InvokerForMatch() mcps.Invoker
	InvokerForInvoke(invoker *CapabilityInvoker) mcps.Invoker
}

// swaggerDoc represents a swagger document structure
type swaggerDoc struct {
	Swagger string `json:"swagger" yaml:"swagger"`
	Info    struct {
		Title string `json:"title" yaml:"title"`
	} `json:"info" yaml:"info"`
	Paths map[string]map[string]struct {
		OperationID string                                `json:"operationId" yaml:"operationId"`
		Summary     string                                `json:"summary" yaml:"summary"`
		Description string                                `json:"description" yaml:"description"`
		Parameters  []capability.SwaggerParam             `json:"parameters" yaml:"parameters"`
		Responses   map[string]capability.SwaggerResponse `json:"responses" yaml:"responses"`
		Tags        []string                              `json:"tags" yaml:"tags"`
	} `json:"paths" yaml:"paths"`
}

// decodeSwaggerDoc decodes swagger document from JSON or YAML format
func decodeSwaggerDoc(r io.Reader) (*swaggerDoc, error) {
	// Read all content first
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}

	doc := new(swaggerDoc)
	// Try JSON first
	if err := json.Unmarshal(data, doc); err == nil && doc.Paths != nil {
		return doc, nil
	}

	// Try YAML
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("decode swagger (tried JSON and YAML): %w", err)
	}
	return doc, nil
}

func (s *capabilityStore) afterCreatedCapability(ctx context.Context, obj *capability.Capability) error {
	subject := obj.GetSubject()
	cvb := capability.CapabilityVectorBasic{
		CapID:   obj.ID,
		Subject: subject,
	}
	vec, err := GetEmbedding(ctx, cvb.Subject)
	if err != nil {
		return err
	}
	if len(vec) > 0 {
		cvb.Vector = vec
	}

	_, err = s.CreateCapabilityVector(ctx, cvb)
	if err != nil {
		logger().Infow("create capability vector fail", "cvb", &cvb, "err", err)
		return err
	}
	return nil
}

func (s *capabilityStore) afterUpdatedCapability(ctx context.Context, doc *capability.Capability) error {
	subject := doc.GetSubject()

	// Check if vector already exists
	existing := new(capability.CapabilityVector)
	err := dbGetWithUnique(ctx, s.w.db, existing, "cap_id", doc.ID)
	if err == nil && existing.Subject == subject {
		logger().Debugw("unchange vector", "subject", subject)
		return nil
	}
	vec, verr := GetEmbedding(ctx, subject)
	if verr != nil {
		logger().Warnw("skip capability due to embedding fail", "id", doc.ID, "err", err)
		return verr // Skip this capability, continue with next
	}
	if err == nil {
		// Update existing
		if existing.Subject != subject {
			logger().Infow("subject changed", "id", doc.ID, "old", existing.Subject, "new", subject)
		}
		existing.SetWith(capability.CapabilityVectorSet{
			Subject: &subject,
			Vector:  &vec,
		})
		if err = dbUpdate(ctx, s.w.db, existing); err != nil {
			return err
		}
	} else {
		// Create new
		cvb := capability.CapabilityVectorBasic{
			CapID:   doc.ID,
			Subject: subject,
			Vector:  vec,
		}
		_, err = s.CreateCapabilityVector(ctx, cvb)
		if err != nil {
			logger().Warnw("create capability vector fail", "capId", doc.ID, "err", err)
			return err
		}
	}
	return nil
}

// afterLoadCapability implements after load hook
func (s *capabilityStore) afterLoadCapability(ctx context.Context, obj *capability.Capability) error {
	return nil
}

// afterListCapability implements after list hook
func (s *capabilityStore) afterListCapability(ctx context.Context, spec *CapCapabilitySpec, data capability.Capabilities) error {
	return nil
}

func dbBeforeDeleteCapability(ctx context.Context, db ormDB, obj *capability.Capability) error {
	_, err := db.NewDelete().Model((*capability.CapabilityVector)(nil)).
		Where("cap_id = ?", obj.ID).Exec(ctx)
	return err
}

func (s *capabilityStore) CountCapability(ctx context.Context) (int, error) {
	spec := &CapCapabilitySpec{}
	spec.Limit = -1
	_, count, err := s.ListCapability(ctx, spec)
	return count, err
}

func (s *capabilityStore) GetCapabilityWith(ctx context.Context, method, endpoint string) (*capability.Capability, error) {
	obj := new(capability.Capability)
	err := dbGet(ctx, s.w.db, obj, "method = ? AND endpoint = ?", strings.ToUpper(method), endpoint)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// MatchVectorWith matches capabilities using vector
func (s *capabilityStore) MatchVectorWith(ctx context.Context, vec corpus.Vector, threshold float32, limit int) (data []capability.CapabilityMatch, err error) {
	if len(vec) != corpus.VectorLen {
		logger().Infow("mismatch length of vector", "a", len(vec), "b", corpus.VectorLen)
		return
	}
	logger().Debugw("match capability with", "vec", vec[0:5])
	err = s.w.db.NewRaw("SELECT * FROM vector_match_capability_4(?, ?, ?)", vec, threshold, limit).
		Scan(ctx, &data)
	if err != nil {
		logger().Infow("match capability vector fail", "threshold", threshold, "limit", limit, "err", err)
	} else {
		logger().Debugw("match capability vector ok", "threshold", threshold, "limit", limit, "data", data)
	}
	return
}

// MatchCapabilities matches capabilities by query
func (s *capabilityStore) MatchCapabilities(ctx context.Context, ms MatchSpec) (data capability.Capabilities, err error) {
	ms.setDefaults()

	var subject string
	if ms.SkipKeywords {
		subject = ms.Query
	} else {
		subject, err = GetSummary(ctx, ms.Query, GetTemplateForKeyword())
		if err != nil {
			return
		}
	}

	if len(subject) == 0 {
		logger().Infow("empty subject", "spec", ms)
		return
	}

	vec, err := GetEmbedding(ctx, subject)
	if err != nil {
		logger().Infow("GetEmbedding fail", "err", err)
		return
	}
	if len(vec) != corpus.VectorLen {
		logger().Infow("embedding length mismatch", "a", len(vec), "b", corpus.VectorLen)
		return
	}

	matches, err := s.MatchVectorWith(ctx, vec, ms.Threshold, ms.Limit)
	if err != nil || len(matches) == 0 {
		logger().Infow("no match capabilities", "subj", subject)
		return
	}

	ids := make(oid.OIDs, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.DocID)
	}
	logger().Infow("matched", "caps", ids, "err", err)

	spec := &CapCapabilitySpec{}
	spec.IDs = ids
	err = queryList(ctx, s.w.db, spec, &data).Scan(ctx)
	if err != nil {
		logger().Infow("list capabilities fail", "spec", spec, "err", err)
	}

	return
}

// SyncEmbeddingCapabilities generates vectors for capabilities
func (s *capabilityStore) SyncEmbeddingCapabilities(ctx context.Context, spec *CapCapabilitySpec) error {
	data, _, err := s.ListCapability(ctx, spec)
	if err != nil {
		return err
	}

	for _, doc := range data {
		_ = s.afterUpdatedCapability(ctx, &doc)
	}
	return nil
}

// ImportCapabilities imports capabilities from swagger document (supports both JSON and YAML formats)
func (s *capabilityStore) ImportCapabilities(ctx context.Context, r io.Reader, lw io.Writer, markMissingPrefix string) error {
	doc, err := decodeSwaggerDoc(r)
	if err != nil {
		logger().Infow("decode swagger fail", "err", err)
		return err
	}

	var imported, skipped int
	// Collect all (method, endpoint) from the import file
	importedEndpoints := make(map[string]bool)
	for path, methods := range doc.Paths {
		for method := range methods {
			method = strings.ToUpper(method)
			if method == "PARAMETERS" || method == "RESOLUTIONS" {
				continue
			}
			importedEndpoints[method+":"+path] = true
		}
	}

	for path, methods := range doc.Paths {
		for method, api := range methods {
			method = strings.ToUpper(method)
			if method == "PARAMETERS" || method == "RESOLUTIONS" {
				continue
			}

			// Try to find existing by method+endpoint (unique constraint)
			existing, err := s.GetCapabilityWith(ctx, method, path)
			if err != nil && !errors.Is(err, ErrNoRows) {
				logger().Warnw("check existing fail", "path", path, "method", method, "err", err)
				continue
			}

			// Skip APIs tagged with skipai, delete existing if found
			if slices.ContainsFunc(api.Tags, func(s string) bool {
				return s == "skipai" || s == "skipAI"
			}) {
				if existing != nil && existing.ID.Valid() {
					if err := s.DeleteCapability(ctx, existing.StringID()); err != nil {
						logger().Infow("delete skipai capability fail", "path", path, "method", method, "err", err)
					} else if lw != nil {
						fmt.Fprintf(lw, "%s %s [deleted]\n", method, path)
					}
				}
				skipped++
				continue
			}

			basic := capability.CapabilityBasic{
				OperationID: api.OperationID,
				Endpoint:    path,
				Method:      method,
				Summary:     api.Summary,
				Description: api.Description,
				Tags:        api.Tags,
			}

			// Assign parameters and responses (filter out token header param)
			basic.Parameters = capability.FilterParams(api.Parameters, "token")
			basic.Responses = api.Responses

			// Skip if no valid subject (no tags, summary, or description)
			basic.EnrichSortableFields()
			if basic.GetSubject() == "" {
				if lw != nil {
					fmt.Fprintf(lw, "%s %s [skipped: empty subject]\n", method, path)
				}
				skipped++
				continue
			}

			if existing != nil && existing.ID.Valid() {
				// Update existing
				err = s.UpdateCapability(ctx, existing.StringID(), capability.CapabilitySet{
					OperationID: &api.OperationID,
					Summary:     &api.Summary,
					Description: &api.Description,
					Parameters:  &basic.Parameters,
					Responses:   &basic.Responses,
					Tags:        &api.Tags,
				})
				if err != nil {
					logger().Warnw("update capability fail", "path", path, "method", method, "err", err)
					skipped++
					continue
				}
				if lw != nil {
					fmt.Fprintf(lw, "%s %s [updated]\n", method, path)
				}
			} else {
				// Create new
				_, err = s.CreateCapability(ctx, basic)
				if err != nil {
					logger().Warnw("create capability fail", "path", path, "method", method, "err", err)
					skipped++
					continue
				}
				if lw != nil {
					fmt.Fprintf(lw, "%s %s [created]\n", method, path)
				}
			}
			imported++
		}
	}

	// Mark missing capabilities
	var missed int
	if markMissingPrefix != "" {
		missed, err = s.markMissingCapabilities(ctx, lw, markMissingPrefix, importedEndpoints)
		if err != nil {
			logger().Warnw("mark missing fail", "err", err)
		}
	}

	logger().Infow("import swagger", "imported", imported, "skipped", skipped, "missed", missed)
	return nil
}

// markMissingCapabilities marks capabilities whose endpoint matches prefix but not in imported file as missed
func (s *capabilityStore) markMissingCapabilities(ctx context.Context, lw io.Writer, prefix string, importedEndpoints map[string]bool) (int, error) {
	// Query all capabilities with endpoint starting with prefix
	var caps capability.Capabilities
	err := s.w.db.NewSelect().Model(&caps).
		Where("endpoint LIKE ?", prefix+"%").
		Scan(ctx)
	if err != nil {
		return 0, err
	}

	missed := 0
	for _, ca := range caps {
		key := ca.Method + ":" + ca.Endpoint
		if _, exists := importedEndpoints[key]; !exists {
			// Not in imported file, mark as missed
			set := capability.CapabilitySet{}
			set.MetaAddKVs("missed", "yes")
			if err := s.UpdateCapability(ctx, ca.StringID(), set); err != nil {
				logger().Warnw("mark missed fail", "id", ca.StringID(), "err", err)
				continue
			}
			missed++
			if lw != nil {
				fmt.Fprintf(lw, "%s %s [missed]\n", ca.Method, ca.Endpoint)
			}
		}
	}
	return missed, nil
}

// InvokerForMatch returns an invoker for matching capabilities
func (s *capabilityStore) InvokerForMatch() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		intent, ok := args["intent"].(string)
		if !ok || intent == "" {
			return mcps.BuildToolErrorResult("missing required argument: intent"), nil
		}

		limit := 6
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}

		caps, err := s.MatchCapabilities(ctx, MatchSpec{
			Query: intent,
			Limit: limit,

			SkipKeywords: true,
		})
		if err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}
		if len(caps) == 0 {
			return mcps.BuildToolSuccessResult("No matching APIs found"), nil
		}
		logger().Infow("matched", "caps", len(caps), "endpoints", caps.Endpoints())

		// Build result with capability details
		result := make([]map[string]any, 0, len(caps))
		for _, cap := range caps {
			result = append(result, map[string]any{
				"id":           cap.StringID(),
				"operation_id": cap.OperationID,
				"endpoint":     cap.Endpoint,
				"method":       cap.Method,
				"summary":      cap.Summary,
				"description":  cap.Description,
				"parameters":   cap.Parameters,
				"subject":      cap.GetSubject(),
			})
		}
		return mcps.BuildToolSuccessResult(result), nil
	}
}

// InvokerForInvoke returns an invoker for invoking capabilities
func (s *capabilityStore) InvokerForInvoke(invoker *CapabilityInvoker) mcps.Invoker {

	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		method, _ := args["method"].(string)
		if method == "" {
			return mcps.BuildToolErrorResult("missing required argument: method"), nil
		}

		endpoint, _ := args["endpoint"].(string)
		if endpoint == "" {
			return mcps.BuildToolErrorResult("missing required argument: endpoint"), nil
		}

		params, _ := args["params"].(map[string]any)
		if params == nil {
			params = make(map[string]any)
		}

		resp, err := invoker.Invoke(ctx, method, endpoint, params)
		if err != nil {
			logger().Infow("invoke fail", "err", err)
			return mcps.BuildToolErrorResult(err.Error()), nil
		}
		if resp == nil {
			return mcps.BuildToolErrorResult("nil response from invoker"), nil
		}
		defer resp.Body.Close()

		result := map[string]any{}

		err = json.NewDecoder(resp.Body).Decode(&result)
		if err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}

		if resp.StatusCode >= 400 {
			logger().Infow("invoked", method, endpoint, "status", resp.StatusCode, "result", result)
			if resp.StatusCode == 403 {
				return mcps.BuildToolErrorResult("Permission denied: no access to this API"), nil
			}
			return mcps.BuildToolErrorResult(
				fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, resp.Status),
			), nil
		}
		logger().Debugw("invoked", method, endpoint, "response", result)

		resultKey := settings.Current.BusResult
		if len(resultKey) > 0 {
			if res, ok := result[resultKey]; ok {
				return mcps.BuildToolSuccessResult(res), nil
			}
		}
		return mcps.BuildToolSuccessResult(result), nil
	}
}

// CleanupMissedCapabilities deletes capabilities marked as missed
func (s *capabilityStore) CleanupMissedCapabilities(ctx context.Context, lw io.Writer, prefix string, dryRun bool) error {
	var caps capability.Capabilities
	q := s.w.db.NewSelect().Model(&caps).Where("meta->>'missed' = ?", "yes")
	if prefix != "" {
		q = q.Where("endpoint LIKE ?", prefix+"%")
	}
	if err := q.Scan(ctx); err != nil {
		return err
	}

	for _, ca := range caps {
		if !dryRun {
			if err := s.DeleteCapability(ctx, ca.StringID()); err != nil {
				if lw != nil {
					fmt.Fprintf(lw, "%s %s %q [delete fail: %v]\n",
						ca.Method, ca.Endpoint, ca.Summary, err)
				}
				logger().Warnw("delete missed capability fail", "id", ca.StringID(), "err", err)
				continue
			}
		}
		if lw != nil {
			fmt.Fprintf(lw, "%s %s %q", ca.Method, ca.Endpoint, ca.Summary)
			if dryRun {
				fmt.Fprintf(lw, " [dry-run]\n")
			} else {
				fmt.Fprintf(lw, " [deleted]\n")
			}
		}
	}
	return nil
}
