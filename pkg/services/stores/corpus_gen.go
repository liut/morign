// This file is generated - Do Not Edit.

package stores

import (
	"context"

	"github.com/liut/morign/pkg/models/corpus"
)

// type ChatLog = corpus.ChatLog
// type DocMatch = corpus.DocMatch
// type DocMatches = corpus.DocMatches
// type CobDocVector = corpus.DocVector
// type CobDocument = corpus.Document
// type ImportFailure = corpus.ImportFailure
// type ImportFailures = corpus.ImportFailures
// type CobImportTask = corpus.ImportTask

func init() {
	RegisterModel((*corpus.Document)(nil), (*corpus.DocVector)(nil), (*corpus.ChatLog)(nil), (*corpus.ImportTask)(nil))
}

type CorpuStore interface {
	CorpuStoreX

	ListDocument(ctx context.Context, spec *CobDocumentSpec) (data corpus.Documents, total int, err error)
	GetDocument(ctx context.Context, id string) (obj *corpus.Document, err error)
	CreateDocument(ctx context.Context, in corpus.DocumentBasic) (obj *corpus.Document, err error)
	UpdateDocument(ctx context.Context, id string, in corpus.DocumentSet) error
	DeleteDocument(ctx context.Context, id string) error

	GetDocVector(ctx context.Context, id string) (obj *corpus.DocVector, err error)
	CreateDocVector(ctx context.Context, in corpus.DocVectorBasic) (obj *corpus.DocVector, err error)
	DeleteDocVector(ctx context.Context, id string) error

	CreateChatLog(ctx context.Context, in corpus.ChatLogBasic) (obj *corpus.ChatLog, err error)
	GetChatLog(ctx context.Context, id string) (obj *corpus.ChatLog, err error)
	ListChatLog(ctx context.Context, spec *ChatLogSpec) (data corpus.ChatLogs, total int, err error)
	DeleteChatLog(ctx context.Context, id string) error

	ListImportTask(ctx context.Context, spec *CobImportTaskSpec) (data corpus.ImportTasks, total int, err error)
	GetImportTask(ctx context.Context, id string) (obj *corpus.ImportTask, err error)
	CreateImportTask(ctx context.Context, in corpus.ImportTaskBasic) (obj *corpus.ImportTask, err error)
	UpdateImportTask(ctx context.Context, id string, in corpus.ImportTaskSet) error
	DeleteImportTask(ctx context.Context, id string) error
}

type CobDocumentSpec struct {
	PageSpec
	ModelSpec

	// 主标题 名称
	Title string `extensions:"x-order=A" form:"title" json:"title"`
	// 内容搜索关键词（走向量匹配，不做 like）
	Match string `extensions:"x-order=B" form:"match" json:"match,omitempty"`
}

func (spec *CobDocumentSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftMatch(q, "title", spec.Title, false)

	return q
}
func (spec *CobDocumentSpec) CanSort(k string) bool {
	switch k {
	case "heading":
		return true
	default:
		return spec.ModelSpec.CanSort(k)
	}
}

type ChatLogSpec struct {
	PageSpec
	ModelSpec

	// 会话ID
	ChatID string `extensions:"x-order=A" form:"csid" json:"csid"`
}

func (spec *ChatLogSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftOID(q, "csid", spec.ChatID, false)

	return q
}

type CobImportTaskSpec struct {
	PageSpec
	ModelSpec

	// 原始文件名
	Filename string `extensions:"x-order=A" form:"filename" json:"filename"`
	// 任务状态
	//  * `pending` - 排队中
	//  * `processing` - 处理中
	//  * `succeeded` - 已完成
	//  * `failed` - 已失败
	Status corpus.ImportTaskStatus `extensions:"x-order=B" form:"status" json:"status" swaggertype:"string"`
}

func (spec *CobImportTaskSpec) Sift(q *ormQuery) *ormQuery {
	q = spec.ModelSpec.Sift(q)
	q, _ = siftMatch(q, "filename", spec.Filename, false)
	q, _ = siftEqual(q, "status", spec.Status, false)

	return q
}
func (spec *CobImportTaskSpec) CanSort(k string) bool {
	switch k {
	case "filename", "status":
		return true
	default:
		return spec.ModelSpec.CanSort(k)
	}
}

type corpuStore struct {
	w *Wrap
}

func (s *corpuStore) ListDocument(ctx context.Context, spec *CobDocumentSpec) (data corpus.Documents, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *corpuStore) GetDocument(ctx context.Context, id string) (obj *corpus.Document, err error) {
	obj = new(corpus.Document)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *corpuStore) CreateDocument(ctx context.Context, in corpus.DocumentBasic) (obj *corpus.Document, err error) {
	obj = corpus.NewDocumentWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	if err == nil {
		err = s.afterCreatedCobDocument(ctx, obj)
	}
	return
}
func (s *corpuStore) UpdateDocument(ctx context.Context, id string, in corpus.DocumentSet) error {
	exist := new(corpus.Document)
	if err := dbGetWithPKID(ctx, s.w.db, exist, id); err != nil {
		return err
	}
	exist.SetIsUpdate(true)
	exist.SetWith(in)
	dbMetaUp(ctx, s.w.db, exist)
	return dbUpdate(ctx, s.w.db, exist)
}
func (s *corpuStore) DeleteDocument(ctx context.Context, id string) error {
	obj := new(corpus.Document)
	if err := dbGetWithPKID(ctx, s.w.db, obj, id); err != nil {
		if errorIs(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		err = dbDeleteM(ctx, tx, s.w.db.Schema(), s.w.db.SchemaCrap(), obj)
		if err != nil {
			return
		}
		return dbAfterDeleteCobDocument(ctx, tx, obj)
	})
}

func (s *corpuStore) GetDocVector(ctx context.Context, id string) (obj *corpus.DocVector, err error) {
	obj = new(corpus.DocVector)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *corpuStore) CreateDocVector(ctx context.Context, in corpus.DocVectorBasic) (obj *corpus.DocVector, err error) {
	obj = corpus.NewDocVectorWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	return
}
func (s *corpuStore) DeleteDocVector(ctx context.Context, id string) error {
	obj := new(corpus.DocVector)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func (s *corpuStore) CreateChatLog(ctx context.Context, in corpus.ChatLogBasic) (obj *corpus.ChatLog, err error) {
	obj = corpus.NewChatLogWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	return
}
func (s *corpuStore) GetChatLog(ctx context.Context, id string) (obj *corpus.ChatLog, err error) {
	obj = new(corpus.ChatLog)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *corpuStore) ListChatLog(ctx context.Context, spec *ChatLogSpec) (data corpus.ChatLogs, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *corpuStore) DeleteChatLog(ctx context.Context, id string) error {
	obj := new(corpus.ChatLog)
	return s.w.db.DeleteModel(ctx, obj, id)
}

func (s *corpuStore) ListImportTask(ctx context.Context, spec *CobImportTaskSpec) (data corpus.ImportTasks, total int, err error) {
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}
func (s *corpuStore) GetImportTask(ctx context.Context, id string) (obj *corpus.ImportTask, err error) {
	obj = new(corpus.ImportTask)
	err = dbGetWithPKID(ctx, s.w.db, obj, id)

	return
}
func (s *corpuStore) CreateImportTask(ctx context.Context, in corpus.ImportTaskBasic) (obj *corpus.ImportTask, err error) {
	obj = corpus.NewImportTaskWithBasic(in)
	dbMetaUp(ctx, s.w.db, obj)
	err = dbInsert(ctx, s.w.db, obj)
	return
}
func (s *corpuStore) UpdateImportTask(ctx context.Context, id string, in corpus.ImportTaskSet) error {
	exist := new(corpus.ImportTask)
	if err := dbGetWithPKID(ctx, s.w.db, exist, id); err != nil {
		return err
	}
	exist.SetIsUpdate(true)
	exist.SetWith(in)
	dbMetaUp(ctx, s.w.db, exist)
	return dbUpdate(ctx, s.w.db, exist)
}
func (s *corpuStore) DeleteImportTask(ctx context.Context, id string) error {
	obj := new(corpus.ImportTask)
	return s.w.db.DeleteModel(ctx, obj, id)
}
