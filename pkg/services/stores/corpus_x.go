package stores

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cupogo/andvari/models/oid"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cast"

	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/settings"
	"github.com/liut/morign/pkg/utils/words"
)

const (
	Separator = "\n* "

	maxImportErrors    = 500
	maxImportReasonLen = 200
)

var (
	ErrEmptyParam = errors.New("empty param")

	qaHeads = []string{"title", "heading", "content"}

	// replText converts Unicode line separator (U+2028) to regular newline
	// This is needed because some Excel cells use U+2028 instead of \n
	replText = strings.NewReplacer("\u2028", "\n")
)

// MatchSpec defines the document matching specification
type MatchSpec struct {
	Query        string
	Threshold    float32
	Limit        int
	SkipKeywords bool
}

// setDefaults sets default threshold and limit
func (ms *MatchSpec) setDefaults() {
	if ms.Threshold == 0 {
		ms.Threshold = settings.Current.VectorThreshold
	}
	if ms.Limit == 0 {
		ms.Limit = settings.Current.VectorLimit
	}
}

// ExportArg is the arguments for document export
type ExportArg struct {
	Spec   *CobDocumentSpec
	Out    io.Writer
	Format string // csv,jsonl
}

// ValidHead validates if CSV header is valid
func ValidHead(rec []string) bool {
	if len(rec) < len(qaHeads) {
		return false
	}
	// 支持小写和首字母大写格式
	for i, expected := range qaHeads {
		if strings.ToLower(rec[i]) != expected {
			logger().Infow("mismatch", "a", rec[i], "b", expected)
			return false
		}
	}
	return true
}

// CorpuStoreX is the knowledge base storage extension interface
type CorpuStoreX interface {
	ImportDocs(ctx context.Context, r io.Reader, lw io.Writer) error
	ExportDocs(ctx context.Context, ea ExportArg) error
	SyncEmbeddingDocments(ctx context.Context, spec *CobDocumentSpec) error
	MatchDocments(ctx context.Context, ms MatchSpec) (data corpus.Documents, err error)
	MatchVectorWith(ctx context.Context, vec corpus.Vector, threshold float32, limit int) (data corpus.DocMatches, err error)
	ClaimImportTask(ctx context.Context) (obj *corpus.ImportTask, err error)
	ProcessImportTask(ctx context.Context, task *corpus.ImportTask) error
	RecoverImportTasks(ctx context.Context) (int, error)
	InvokerForSearch() mcps.Invoker
	InvokerForCreate() mcps.Invoker
}

// ImportDocs imports documents from CSV
func (s *corpuStore) ImportDocs(ctx context.Context, r io.Reader, lw io.Writer) error {
	rd := csv.NewReader(r)
	rec, err := rd.Read()
	if err != nil {
		logger().Infow("read fail", "err", err)
		return err
	}
	if !ValidHead(rec) {
		return fmt.Errorf("invalid csv head: %+v", rec)
	}

	var idx int
	var valid int
	for {
		row, err := rd.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger().Infow("import docs", "lines", idx, "valid", valid)
				return nil
			}
			return err
		}
		idx++
		if len(row) < 3 || len(row[0]) == 0 || len(row[1]) == 0 || len(row[2]) == 0 {
			logger().Infow("empty row, skip", "idx", idx, "row", row)
			// return fmt.Errorf("invalid csv row #%d: %+v", idx, row)
			continue
		}
		err = s.importLine(ctx, corpus.DocumentBasic{
			Title:   row[0],
			Heading: row[1],
			Content: row[2],
		}, lw)
		if err != nil {
			return err
		}
		valid++
	}
}

// ClaimImportTask 原子认领最早的 pending 导入任务，无任务时返回 ErrNotFound
func (s *corpuStore) ClaimImportTask(ctx context.Context) (obj *corpus.ImportTask, err error) {
	obj = new(corpus.ImportTask)
	err = s.w.db.NewUpdate().
		Model(obj).
		Set("status = ?", corpus.ImportTaskStatusProcessing).
		Set("started_at = ?", time.Now()).
		Where("status = ?", corpus.ImportTaskStatusPending).
		Where("id = (SELECT id FROM ? WHERE status = ? ORDER BY created, id LIMIT 1 FOR UPDATE SKIP LOCKED)",
			pgIdent(corpus.ImportTaskTable), corpus.ImportTaskStatusPending).
		Returning("*").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, ErrNotFound
		}
		logger().Infow("claim import task fail", "err", err)
		return nil, err
	}
	return obj, nil
}

// ProcessImportTask 处理单个导入任务：解析 CSV、逐行查重/导入并写回计数、明细与状态
func (s *corpuStore) ProcessImportTask(ctx context.Context, task *corpus.ImportTask) error {
	rd := csv.NewReader(strings.NewReader(task.Data))
	rd.FieldsPerRecord = -1

	rec, err := rd.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			// 空文件：正常结束，计数全 0
			return s.finishImportTask(ctx, task, corpus.ImportTaskStatusSucceeded, 0, 0, 0, 0, nil)
		}
		return s.finishImportTask(ctx, task, corpus.ImportTaskStatusFailed, 0, 0, 0, 0, nil)
	}
	if !ValidHead(rec) {
		logger().Infow("invalid import task head", "id", task.StringID())
		return s.finishImportTask(ctx, task, corpus.ImportTaskStatusFailed, 0, 0, 0, 0, nil)
	}

	var (
		total, success, failed, skipped int
		errs                            []corpus.ImportFailure
	)
	for {
		row, err := rd.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// CSV 中途解析错误 → 任务失败
			logger().Infow("import task csv parse fail", "id", task.StringID(), "err", err)
			return s.finishImportTask(ctx, task, corpus.ImportTaskStatusFailed, total, success, failed, skipped, errs)
		}
		line, _ := rd.FieldPos(0)
		total++
		if !validImportRow(row) {
			failed++
			errs = appendImportError(errs, corpus.ImportFailure{
				Line:   line,
				Title:  importRowTitle(row),
				Reason: "数据无效",
				Kind:   corpus.ImportFailureKindFailed,
			})
			continue
		}

		basic := corpus.DocumentBasic{
			Title:   row[0],
			Heading: row[1],
			Content: replText.Replace(row[2]),
		}
		exist := new(corpus.Document)
		err = dbGet(ctx, s.w.db, exist, "title = ? AND heading = ?", basic.Title, basic.Heading)
		switch {
		case err == nil:
			skipped++
			errs = appendImportError(errs, corpus.ImportFailure{
				Line:   line,
				Title:  basic.Title,
				Reason: "重复文档: title+heading 已存在，跳过",
				Kind:   corpus.ImportFailureKindSkipped,
			})
		case errors.Is(err, ErrNotFound):
			if _, cerr := s.CreateDocument(ctx, basic); cerr != nil {
				failed++
				errs = appendImportError(errs, corpus.ImportFailure{
					Line:   line,
					Title:  basic.Title,
					Reason: cerr.Error(),
					Kind:   corpus.ImportFailureKindFailed,
				})
			} else {
				success++
			}
		default:
			failed++
			errs = appendImportError(errs, corpus.ImportFailure{
				Line:   line,
				Title:  basic.Title,
				Reason: err.Error(),
				Kind:   corpus.ImportFailureKindFailed,
			})
		}
	}

	return s.finishImportTask(ctx, task, corpus.ImportTaskStatusSucceeded, total, success, failed, skipped, errs)
}

// RecoverImportTasks 将遗留的 processing 任务重置为 pending，返回受影响数量
func (s *corpuStore) RecoverImportTasks(ctx context.Context) (int, error) {
	res, err := s.w.db.NewUpdate().
		Model((*corpus.ImportTask)(nil)).
		Set("status = ?", corpus.ImportTaskStatusPending).
		Where("status = ?", corpus.ImportTaskStatusProcessing).
		Exec(ctx)
	if err != nil {
		logger().Infow("recover import tasks fail", "err", err)
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func validImportRow(row []string) bool {
	return len(row) >= 3 && len(row[0]) > 0 && len(row[1]) > 0 && len(row[2]) > 0
}

func importRowTitle(row []string) string {
	if len(row) > 0 {
		return row[0]
	}
	return ""
}

func appendImportError(errs []corpus.ImportFailure, f corpus.ImportFailure) []corpus.ImportFailure {
	if r := []rune(f.Reason); len(r) > maxImportReasonLen {
		f.Reason = string(r[:maxImportReasonLen])
	}
	if len(errs) >= maxImportErrors {
		return errs
	}
	return append(errs, f)
}

func (s *corpuStore) finishImportTask(ctx context.Context, task *corpus.ImportTask,
	status corpus.ImportTaskStatus, total, success, failed, skipped int, errs []corpus.ImportFailure) error {
	now := time.Now()
	return s.UpdateImportTask(ctx, task.StringID(), corpus.ImportTaskSet{
		Status:     &status,
		Total:      &total,
		Success:    &success,
		Failed:     &failed,
		Skipped:    &skipped,
		Errors:     &errs,
		FinishedAt: &now,
	})
}

// importLine imports a single line of document data
func (s *corpuStore) importLine(ctx context.Context, basic corpus.DocumentBasic, lw io.Writer) error {
	doc := new(corpus.Document)
	basic.Content = replText.Replace(basic.Content)
	err := dbGet(ctx, s.w.db, doc, "title = ? AND heading = ?", basic.Title, basic.Heading)
	if err != nil {
		doc, err = s.CreateDocument(ctx, basic)
	} else {
		if doc.Content != basic.Content {
			logger().Infow("updating", "id", doc.ID, "title", basic.Title, "heading", basic.Heading)
			dif := diff2(doc.Content, basic.Content)
			if lw != nil {
				if _, werr := fmt.Fprintf(lw, "Doc ID: %s, Title: %s, Heading: %s\nDiff:\n%s\n\n",
					doc.StringID(), basic.Title, basic.Heading, dif); werr != nil {
					logger().Infow("write diff fail", "lw", lw, "err", werr)
				}
			}

		}
		err = s.UpdateDocument(ctx, doc.StringID(), corpus.DocumentSet{
			Content: &basic.Content,
		})
	}
	if err != nil {
		logger().Infow("save document fail", "id", doc.ID, "err", err)
		return err
	}
	return nil
}

// diff2 calculates the difference between two texts
func diff2(text1, text2 string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(text1),
		B:        difflib.SplitLines(text2),
		FromFile: "Original",
		ToFile:   "Current",
		Context:  3,
	}
	text, _ := difflib.GetUnifiedDiffString(diff)
	return text
}

// afterCreatedCobDocument generates vector after document creation
func (s *corpuStore) afterCreatedCobDocument(ctx context.Context, obj *corpus.Document) error {
	dvb := corpus.DocVectorBasic{
		DocID:   obj.ID,
		Subject: obj.GetSubject(),
	}
	vec, err := GetEmbedding(ctx, dvb.Subject)
	if err != nil {
		return err
	}
	if len(vec) > 0 {
		dvb.Vector = vec
	}

	_, err = s.CreateDocVector(ctx, dvb)
	if err != nil {
		logger().Infow("create doc vector fail", "dvb", &dvb, "err", err)
		return err
	}
	return nil
}

// GetEmbedding gets the vector representation of text
func GetEmbedding(ctx context.Context, text string) (vec corpus.Vector, err error) {
	if len(text) == 0 {
		err = ErrEmptyParam
		return
	}

	// 使用 embedding client
	embedding, err := GetLLMEmbeddingClient().Embedding(ctx, []string{text})
	if err != nil {
		logger().Infow("embedding fail", "text", text, "err", err)
		return
	}
	if len(embedding) > 0 {
		// 转换 []float64 到 []float32
		vec = make(corpus.Vector, len(embedding))
		for i, v := range embedding {
			vec[i] = float32(v)
		}
		logger().Infow("embedding res", "text", words.TakeHead(text, 60, ".."), "vec", len(vec))
	} else {
		logger().Infow("embedding result is empty", "text", text)
	}
	return
}

// MatchDocments matches documents
func (s *corpuStore) MatchDocments(ctx context.Context, ms MatchSpec) (data corpus.Documents, err error) {
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
	ps, err := matchVectors(ctx, s, subject, ms.Threshold, ms.Limit)
	if err != nil || len(ps) == 0 {
		logger().Infow("no match docs", "subj", subject, "err", err)
		return
	}
	logger().Infow("matched", "docs", ps.Subjects(30), "err", err)
	spec := &CobDocumentSpec{}
	spec.IDs = ps.DocumentIDs()
	err = queryList(ctx, s.w.db, spec, &data).Scan(ctx)
	if err != nil {
		logger().Infow("list docs fail", "spec", spec, "err", err)
	} else {
		logger().Infow("list docs", "ids", spec.IDs, "matches", data.Headings())
	}
	return
}

const (
	// defaultMatchCandidates 列表语义搜索的向量候选数量
	defaultMatchCandidates = 50
	// minMatchRunes 去除空格后的搜索词最少字符数
	minMatchRunes = 2
)

// SiftX 处理需要上下文或外部服务的查询条件：spec.Match 走向量匹配
func (spec *CobDocumentSpec) SiftX(ctx context.Context, q *ormQuery) *ormQuery {
	if len(spec.Match) == 0 {
		return q
	}
	keyword, ok := matchKeyword(spec.Match)
	if !ok {
		logger().Infow("match too short", "match", spec.Match)
		return q.Where("FALSE")
	}
	ps, err := matchVectors(ctx, Sgt().Corpus(), keyword, settings.Current.VectorThreshold, spec.matchCandidates())
	if err != nil {
		logger().Infow("match fail", "match", keyword, "err", err)
		return q.Err(err)
	}
	return siftMatchIDs(q, ps.DocumentIDs())
}

// vectorMatcher 向量匹配所需的最小接口
type vectorMatcher interface {
	MatchVectorWith(ctx context.Context, vec corpus.Vector, threshold float32, limit int) (data corpus.DocMatches, err error)
}

// matchVectors 由查询词得到向量匹配结果
func matchVectors(ctx context.Context, m vectorMatcher, query string, threshold float32, limit int) (corpus.DocMatches, error) {
	vec, err := GetEmbedding(ctx, query)
	if err != nil {
		logger().Infow("GetEmbedding fail", "query", query, "err", err)
		return nil, err
	}
	return m.MatchVectorWith(ctx, vec, threshold, limit)
}

// matchKeyword 返回去除空格后的搜索词，不足 minMatchRunes 时返回 false
func matchKeyword(match string) (string, bool) {
	keyword := strings.Join(strings.Fields(match), "")
	return keyword, utf8.RuneCountInString(keyword) >= minMatchRunes
}

// siftMatchIDs 按命中的文档编号过滤
func siftMatchIDs(q *ormQuery, ids oid.OIDs) *ormQuery {
	if len(ids) == 0 {
		return q.Where("FALSE")
	}
	q, _ = sift(q, "id", "in", ids, false)
	return q
}

// matchCandidates 向量候选数量：覆盖当前页所需，至少 defaultMatchCandidates
func (spec *CobDocumentSpec) matchCandidates() int {
	if n := spec.GetLimit() + spec.GetSkip(); n > defaultMatchCandidates {
		return n
	}
	return defaultMatchCandidates
}

// MatchVectorWith matches documents using vector
func (s *corpuStore) MatchVectorWith(ctx context.Context, vec corpus.Vector, threshold float32, limit int) (data corpus.DocMatches, err error) {
	if len(vec) != corpus.VectorLen {
		logger().Infow("mismatch length of vector", "a", len(vec), "b", corpus.VectorLen)
		return
	}
	logger().Debugw("match with", "vec", vec[0:5])
	err = s.w.db.NewRaw("SELECT * FROM vector_match_docs_4(?, ?, ?)", vec, threshold, limit).
		Scan(ctx, &data)
	// err = s.w.db.NewSelect().
	// 	Table(corpus.DocVectorTable).
	// 	Column("doc_id", "subject").
	// 	ColumnExpr("(embedding <=> ?) as similarity", vec).
	// 	Where("(embedding <=> ?) < ?", vec, threshold).
	// 	OrderExpr("embedding <=> ?", vec).
	// 	Limit(limit).Scan(ctx, &data)
	if err != nil {
		logger().Infow("match vector fail", "threshold", threshold, "limit", limit, "err", err)
	} else {
		logger().Debugw("match vector ok", "threshold", threshold, "limit", limit, "data", data)
	}
	return
}

// ExportDocs exports documents
func (s *corpuStore) ExportDocs(ctx context.Context, ea ExportArg) error {
	data, _, err := s.ListDocument(ctx, ea.Spec)
	if err != nil {
		return err
	}

	if ea.Format == "csv" {
		return documentsToCSV(data, ea.Out)
	}

	// TODO: jsonl?
	return errors.New("invalid format: " + ea.Format)
}

// documentsToCSV exports document list to CSV format
func documentsToCSV(data corpus.Documents, w io.Writer) error {

	head := []string{"doc_id", "title", "heading", "content"}
	cw := csv.NewWriter(w)
	if err := cw.Write(head); err != nil {
		return err
	}

	for _, doc := range data {
		if err := cw.Write([]string{
			doc.StringID(), doc.Title, doc.Heading, doc.Content,
		}); err != nil {
			return err
		}
	}
	cw.Flush()

	return cw.Error()
}

// SyncEmbeddingDocments generates vectors for documents
func (s *corpuStore) SyncEmbeddingDocments(ctx context.Context, spec *CobDocumentSpec) error {
	data, _, err := s.ListDocument(ctx, spec)
	if err != nil {
		return err
	}

	for _, doc := range data {
		subject := doc.GetSubject()
		contentKeys, err := GetSummary(ctx, doc.Content, GetTemplateForKeyword())
		if err != nil {
			return err
		}
		subject += " " + contentKeys
		vec, err := GetEmbedding(ctx, subject)
		if err != nil {
			return err
		}
		exist := new(corpus.DocVector)
		err = dbGetWithUnique(ctx, s.w.db, exist, "doc_id", doc.ID)
		if err == nil {
			if exist.Subject != subject {
				logger().Infow("changed", "sub1", exist.Subject, "sub2", subject)
			}
			exist.SetWith(corpus.DocVectorSet{
				Subject: &subject,
				Vector:  &vec,
			})
			if err = dbUpdate(ctx, s.w.db, exist); err != nil {
				return err
			}
		} else {
			dv := corpus.NewDocVectorWithBasic(corpus.DocVectorBasic{
				DocID:   doc.ID,
				Subject: subject,
				Vector:  vec,
			})
			if err = dbInsert(ctx, s.w.db, dv); err != nil {
				return err
			}
		}
	}
	return nil
}

// dbAfterDeleteCobDocument cleans up related vector data after document deletion
func dbAfterDeleteCobDocument(ctx context.Context, db ormDB, obj *corpus.Document) error {
	_, err := dbBatchDeleteWithKeyID(ctx, db, corpus.DocVectorTable, "doc_id", obj.ID)
	return err
}

// InvokerForSearch 返回一个搜索知识库文档的 invoker
func (s *corpuStore) InvokerForSearch() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {

		subject, err := cast.ToStringE(args["subject"])
		if err != nil || len(subject) == 0 {
			logger().Infow("kb search fail: empty subject")
			return mcps.BuildToolErrorResult("missing required argument: subject"), nil
		}

		docs, err := s.MatchDocments(ctx, MatchSpec{
			Query:        subject,
			Limit:        5,
			SkipKeywords: true,
		})
		if err != nil {
			return mcps.BuildToolErrorResult(err.Error()), nil
		}
		if len(docs) == 0 {
			logger().Infow("matches not found", "subj", subject)
			return mcps.BuildToolSuccessResult("No relevant information found"), nil
		}
		logger().Infow("matched", "docs", len(docs))

		return mcps.BuildToolSuccessResult(docs.MarkdownText()), nil

	}
}

// InvokerForCreate 返回一个创建知识库文档的 invoker
func (s *corpuStore) InvokerForCreate() mcps.Invoker {
	return func(ctx context.Context, args map[string]any) (map[string]any, error) {
		if !IsKeeper(ctx) {
			return mcps.BuildToolErrorResult("permission denied: keeper role required"), nil
		}

		user, _ := UserFromContext(ctx)
		logger().Infow("mcp call qa create", "args", args, "user", user)

		title, err := cast.ToStringE(args["title"])
		if err != nil || title == "" {
			return mcps.BuildToolErrorResult("missing required argument: title"), nil
		}
		heading, err := cast.ToStringE(args["heading"])
		if err != nil || heading == "" {
			return mcps.BuildToolErrorResult("missing required argument: heading"), nil
		}
		content, err := cast.ToStringE(args["content"])
		if err != nil || content == "" {
			return mcps.BuildToolErrorResult("missing required argument: content"), nil
		}

		docBasic := corpus.DocumentBasic{
			Title:   title,
			Heading: heading,
			Content: content,
		}
		docBasic.MetaAddKVs("creator", user.Name)
		obj, err := s.CreateDocument(ctx, docBasic)
		if err != nil {
			logger().Infow("create document fail", "title", docBasic.Title, "heading", docBasic.Heading,
				"content", len(docBasic.Content), "err", err)
			return mcps.BuildToolSuccessResult(fmt.Sprintf(
				"Create KB document with title %q and heading %q is failed, %s", docBasic.Title, docBasic.Heading, err)), nil
		}
		return mcps.BuildToolSuccessResult(fmt.Sprintf("Created KB document with ID %s", obj.StringID())), nil
	}
}
