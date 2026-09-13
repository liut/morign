// This file is generated - Do Not Edit.

package corpus

import (
	"fmt"
	"time"

	comm "github.com/cupogo/andvari/models/comm"
	oid "github.com/cupogo/andvari/models/oid"
)

// 导入任务状态
type ImportTaskStatus int8

const (
	ImportTaskStatusPending    ImportTaskStatus = 1 + iota //  1 排队中
	ImportTaskStatusProcessing                             //  2 处理中
	ImportTaskStatusSucceeded                              //  3 已完成
	ImportTaskStatusFailed                                 //  4 已失败
)

func (z *ImportTaskStatus) Decode(s string) error {
	switch s {
	case "1", "pending", "Pending":
		*z = ImportTaskStatusPending
	case "2", "processing", "Processing":
		*z = ImportTaskStatusProcessing
	case "3", "succeeded", "Succeeded":
		*z = ImportTaskStatusSucceeded
	case "4", "failed", "Failed":
		*z = ImportTaskStatusFailed
	default:
		return fmt.Errorf("invalid importTaskStatus: %q", s)
	}
	return nil
}
func (z *ImportTaskStatus) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z ImportTaskStatus) String() string {
	switch z {
	case ImportTaskStatusPending:
		return "pending"
	case ImportTaskStatusProcessing:
		return "processing"
	case ImportTaskStatusSucceeded:
		return "succeeded"
	case ImportTaskStatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("importTaskStatus %d", int8(z))
	}
}
func (z ImportTaskStatus) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// 导入明细类型
type ImportFailureKind int8

const (
	ImportFailureKindFailed  ImportFailureKind = 1 + iota //  1 失败
	ImportFailureKindSkipped                              //  2 跳过
)

func (z *ImportFailureKind) Decode(s string) error {
	switch s {
	case "1", "failed", "Failed":
		*z = ImportFailureKindFailed
	case "2", "skipped", "Skipped":
		*z = ImportFailureKindSkipped
	default:
		return fmt.Errorf("invalid importFailureKind: %q", s)
	}
	return nil
}
func (z *ImportFailureKind) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z ImportFailureKind) String() string {
	switch z {
	case ImportFailureKindFailed:
		return "failed"
	case ImportFailureKindSkipped:
		return "skipped"
	default:
		return fmt.Sprintf("importFailureKind %d", int8(z))
	}
}
func (z ImportFailureKind) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// consts of Document 文档
const (
	DocumentTable = "corpus_document"
	DocumentAlias = "cd"
	DocumentLabel = "document"
	DocumentTypID = "corpusDocument"
)

// Document 文档 语料库
type Document struct {
	comm.BaseModel `bun:"table:corpus_document,alias:cd" json:"-"`

	comm.DefaultModel

	DocumentBasic

	// 相似度 仅用于查询结果
	Similarity float32 `bun:"-" extensions:"x-order=D" json:"similarity,omitempty" pg:"-"`

	comm.MetaField
} // @name corpusDocument

type DocumentBasic struct {
	// 主标题 名称
	Title string `bun:",notnull,type:text,unique:corpus_title_heading_key" extensions:"x-order=A" form:"title" json:"title" pg:",notnull,type:text,unique:corpus_title_heading_key"`
	// 小节标题 属性 类别
	Heading string `bun:",notnull,type:text,unique:corpus_title_heading_key" extensions:"x-order=B" form:"heading" json:"heading" pg:",notnull,type:text,unique:corpus_title_heading_key"`
	// 内容 值
	Content string `bun:",notnull,type:text" extensions:"x-order=C" form:"content" json:"content" pg:",notnull,type:text"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name corpusDocumentBasic

type Documents []Document

// Creating function call to it's inner fields defined hooks
func (z *Document) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtArticle))
	}

	return z.DefaultModel.Creating()
}
func NewDocumentWithBasic(in DocumentBasic) *Document {
	obj := &Document{
		DocumentBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewDocumentWithID(id any) *Document {
	obj := new(Document)
	_ = obj.SetID(id)
	return obj
}
func (_ *Document) IdentityLabel() string { return DocumentLabel }
func (_ *Document) IdentityModel() string { return DocumentTypID }
func (_ *Document) IdentityTable() string { return DocumentTable }
func (_ *Document) IdentityAlias() string { return DocumentAlias }

type DocumentSet struct {
	// 主标题 名称
	Title *string `extensions:"x-order=A" json:"title"`
	// 小节标题 属性 类别
	Heading *string `extensions:"x-order=B" json:"heading"`
	// 内容 值
	Content *string `extensions:"x-order=C" json:"content"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name corpusDocumentSet

func (z *Document) SetWith(o DocumentSet) {
	if o.Title != nil && z.Title != *o.Title {
		z.LogChangeValue("title", z.Title, o.Title)
		z.Title = *o.Title
	}
	if o.Heading != nil && z.Heading != *o.Heading {
		z.LogChangeValue("heading", z.Heading, o.Heading)
		z.Heading = *o.Heading
	}
	if o.Content != nil && z.Content != *o.Content {
		z.LogChangeValue("content", z.Content, o.Content)
		z.Content = *o.Content
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *DocumentBasic) MetaAddKVs(args ...any) *DocumentBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *DocumentSet) MetaAddKVs(args ...any) *DocumentSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of DocVector 文档向量
const (
	DocVectorTable = "corpus_vector_400"
	DocVectorAlias = "cv"
	DocVectorLabel = "docVector"
	DocVectorTypID = "corpusDocVector"
)

// DocVector 文档向量 400=1024, 600=1536
type DocVector struct {
	comm.BaseModel `bun:"table:corpus_vector_400,alias:cv" json:"-"`

	comm.DefaultModel

	DocVectorBasic

	// 相似度 仅用于查询结果
	Similarity float32 `bun:"-" extensions:"x-order=D" json:"similarity,omitempty" pg:"-"`

	comm.MetaField
} // @name corpusDocVector

type DocVectorBasic struct {
	// 文档编号
	DocID oid.OID `bun:"doc_id,notnull" extensions:"x-order=A" json:"docID" pg:"doc_id,notnull" swaggertype:"string"`
	// 主题 由名称+属性组成
	Subject string `bun:"subject,notnull,type:text" extensions:"x-order=B" form:"subject" json:"subject" pg:"subject,notnull,type:text"`
	// 向量值 长为1024的浮点数集
	Vector Vector `bun:"embedding,type:vector(1024)" extensions:"x-order=C" json:"vector,omitempty" pg:"embedding,type:vector(1024)"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name corpusDocVectorBasic

type DocVectors []DocVector

// Creating function call to it's inner fields defined hooks
func (z *DocVector) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtEvent))
	}

	return z.DefaultModel.Creating()
}
func NewDocVectorWithBasic(in DocVectorBasic) *DocVector {
	obj := &DocVector{
		DocVectorBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewDocVectorWithID(id any) *DocVector {
	obj := new(DocVector)
	_ = obj.SetID(id)
	return obj
}
func (_ *DocVector) IdentityLabel() string { return DocVectorLabel }
func (_ *DocVector) IdentityModel() string { return DocVectorTypID }
func (_ *DocVector) IdentityTable() string { return DocVectorTable }
func (_ *DocVector) IdentityAlias() string { return DocVectorAlias }

type DocVectorSet struct {
	// 主题 由名称+属性组成
	Subject *string `extensions:"x-order=A" json:"subject"`
	// 向量值 长为1024的浮点数集
	Vector *Vector `extensions:"x-order=B" json:"vector,omitempty"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name corpusDocVectorSet

func (z *DocVector) SetWith(o DocVectorSet) {
	if o.Subject != nil && z.Subject != *o.Subject {
		z.LogChangeValue("subject", z.Subject, o.Subject)
		z.Subject = *o.Subject
	}
	if o.Vector != nil {
		z.LogChangeValue("embedding", z.Vector, o.Vector)
		z.Vector = *o.Vector
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *DocVectorBasic) MetaAddKVs(args ...any) *DocVectorBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *DocVectorSet) MetaAddKVs(args ...any) *DocVectorSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of DocMatch 提示匹配结果
const (
	DocMatchLabel = "docMatch"
	DocMatchTypID = "corpusDocMatch"
)

// DocMatch 提示匹配结果
type DocMatch struct {
	// 文档编号
	DocID oid.OID `bun:"doc_id" extensions:"x-order=A" json:"docID" swaggertype:"string"`
	// 提示
	Subject string `bun:"subject" extensions:"x-order=B" form:"subject" json:"subject"`
	// 相似度
	Similarity float32 `bun:"similarity" extensions:"x-order=C" json:"similarity,omitempty"`
} // @name corpusDocMatch

type DocMatches []DocMatch

// consts of ChatLog 聊天日志
const (
	ChatLogTable = "qa_chat_log"
	ChatLogAlias = "cl"
	ChatLogLabel = "chatLog"
	ChatLogTypID = "corpusChatLog"
)

// ChatLog 聊天日志 Deprecated
type ChatLog struct {
	comm.BaseModel `bun:"table:qa_chat_log,alias:cl" json:"-"`

	comm.DefaultModel

	ChatLogBasic

	comm.MetaField
} // @name corpusChatLog

type ChatLogBasic struct {
	// 会话ID
	ChatID oid.OID `bun:"csid,notnull" extensions:"x-order=A" json:"csid" pg:"csid,notnull" swaggertype:"string"`
	// 提问
	Question string `bun:",notnull,type:text" extensions:"x-order=B" form:"prompt" json:"prompt" pg:",notnull,type:text"`
	// 回答
	Answer string `bun:",notnull,type:text" extensions:"x-order=C" form:"response" json:"response" pg:",notnull,type:text"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name corpusChatLogBasic

type ChatLogs []ChatLog

// Creating function call to it's inner fields defined hooks
func (z *ChatLog) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtEvent))
	}

	return z.DefaultModel.Creating()
}
func NewChatLogWithBasic(in ChatLogBasic) *ChatLog {
	obj := &ChatLog{
		ChatLogBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewChatLogWithID(id any) *ChatLog {
	obj := new(ChatLog)
	_ = obj.SetID(id)
	return obj
}
func (_ *ChatLog) IdentityLabel() string { return ChatLogLabel }
func (_ *ChatLog) IdentityModel() string { return ChatLogTypID }
func (_ *ChatLog) IdentityTable() string { return ChatLogTable }
func (_ *ChatLog) IdentityAlias() string { return ChatLogAlias }

type ChatLogSet struct {
	// 提问
	Question *string `extensions:"x-order=A" json:"prompt"`
	// 回答
	Answer *string `extensions:"x-order=B" json:"response"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name corpusChatLogSet

func (z *ChatLog) SetWith(o ChatLogSet) {
	if o.Question != nil && z.Question != *o.Question {
		z.LogChangeValue("question", z.Question, o.Question)
		z.Question = *o.Question
	}
	if o.Answer != nil && z.Answer != *o.Answer {
		z.LogChangeValue("answer", z.Answer, o.Answer)
		z.Answer = *o.Answer
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *ChatLogBasic) MetaAddKVs(args ...any) *ChatLogBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *ChatLogSet) MetaAddKVs(args ...any) *ChatLogSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of ImportTask 文档导入任务
const (
	ImportTaskTable = "corpus_import"
	ImportTaskAlias = "ci"
	ImportTaskLabel = "importTask"
	ImportTaskTypID = "corpusImportTask"
)

// ImportTask 文档导入任务
type ImportTask struct {
	comm.BaseModel `bun:"table:corpus_import,alias:ci" json:"-"`

	comm.DefaultModel

	ImportTaskBasic

	comm.MetaField
} // @name corpusImportTask

type ImportTaskBasic struct {
	// 原始文件名
	Filename string `bun:",notnull,type:varchar(255)" extensions:"x-order=A" form:"filename" json:"filename" pg:",notnull,type:varchar(255)"`
	// 任务状态
	//  * `pending` - 排队中
	//  * `processing` - 处理中
	//  * `succeeded` - 已完成
	//  * `failed` - 已失败
	Status ImportTaskStatus `bun:",notnull,type:smallint,default:1" enums:"pending,processing,succeeded,failed" extensions:"x-order=B" form:"status" json:"status" pg:",notnull,type:smallint,default:1" swaggertype:"string"`
	// 原始 CSV 内容（worker 读取处理）
	Data string `bun:",notnull,type:text" extensions:"x-order=C" form:"data" json:"data,omitempty" pg:",notnull,type:text"`
	// 总数据行数
	Total int `bun:",notnull,default:0" extensions:"x-order=D" form:"total" json:"total" pg:",notnull,default:0"`
	// 成功行数
	Success int `bun:",notnull,default:0" extensions:"x-order=E" form:"success" json:"success" pg:",notnull,default:0"`
	// 失败行数
	Failed int `bun:",notnull,default:0" extensions:"x-order=F" form:"failed" json:"failed" pg:",notnull,default:0"`
	// 跳过行数（重复行）
	Skipped int `bun:",notnull,default:0" extensions:"x-order=G" form:"skipped" json:"skipped" pg:",notnull,default:0"`
	// 失败明细
	Errors []ImportFailure `bun:",notnull,type:jsonb,default:'[]'" extensions:"x-order=H" json:"errors,omitempty" pg:",notnull,type:jsonb,default:'[]'"`
	// 开始处理时间
	StartedAt *time.Time `bun:"started_at,type:timestamptz" extensions:"x-order=I" json:"startedAt,omitempty" pg:"started_at,type:timestamptz"`
	// 完成时间
	FinishedAt *time.Time `bun:"finished_at,type:timestamptz" extensions:"x-order=J" json:"finishedAt,omitempty" pg:"finished_at,type:timestamptz"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name corpusImportTaskBasic

type ImportTasks []ImportTask

// Creating function call to it's inner fields defined hooks
func (z *ImportTask) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtTask))
	}

	return z.DefaultModel.Creating()
}
func NewImportTaskWithBasic(in ImportTaskBasic) *ImportTask {
	obj := &ImportTask{
		ImportTaskBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewImportTaskWithID(id any) *ImportTask {
	obj := new(ImportTask)
	_ = obj.SetID(id)
	return obj
}
func (_ *ImportTask) IdentityLabel() string { return ImportTaskLabel }
func (_ *ImportTask) IdentityModel() string { return ImportTaskTypID }
func (_ *ImportTask) IdentityTable() string { return ImportTaskTable }
func (_ *ImportTask) IdentityAlias() string { return ImportTaskAlias }

type ImportTaskSet struct {
	// 原始文件名
	Filename *string `extensions:"x-order=A" json:"filename"`
	// 任务状态
	//  * `pending` - 排队中
	//  * `processing` - 处理中
	//  * `succeeded` - 已完成
	//  * `failed` - 已失败
	Status *ImportTaskStatus `enums:"pending,processing,succeeded,failed" extensions:"x-order=B" json:"status" swaggertype:"string"`
	// 原始 CSV 内容（worker 读取处理）
	Data *string `extensions:"x-order=C" form:"data" json:"data,omitempty"`
	// 总数据行数
	Total *int `extensions:"x-order=D" json:"total"`
	// 成功行数
	Success *int `extensions:"x-order=E" json:"success"`
	// 失败行数
	Failed *int `extensions:"x-order=F" json:"failed"`
	// 跳过行数（重复行）
	Skipped *int `extensions:"x-order=G" json:"skipped"`
	// 失败明细
	Errors *[]ImportFailure `extensions:"x-order=H" json:"errors,omitempty"`
	// 开始处理时间
	StartedAt *time.Time `extensions:"x-order=I" json:"startedAt,omitempty"`
	// 完成时间
	FinishedAt *time.Time `extensions:"x-order=J" json:"finishedAt,omitempty"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name corpusImportTaskSet

func (z *ImportTask) SetWith(o ImportTaskSet) {
	if o.Filename != nil && z.Filename != *o.Filename {
		z.LogChangeValue("filename", z.Filename, o.Filename)
		z.Filename = *o.Filename
	}
	if o.Status != nil && z.Status != *o.Status {
		z.LogChangeValue("status", z.Status, o.Status)
		z.Status = *o.Status
	}
	if o.Data != nil && z.Data != *o.Data {
		z.LogChangeValue("data", z.Data, o.Data)
		z.Data = *o.Data
	}
	if o.Total != nil && z.Total != *o.Total {
		z.LogChangeValue("total", z.Total, o.Total)
		z.Total = *o.Total
	}
	if o.Success != nil && z.Success != *o.Success {
		z.LogChangeValue("success", z.Success, o.Success)
		z.Success = *o.Success
	}
	if o.Failed != nil && z.Failed != *o.Failed {
		z.LogChangeValue("failed", z.Failed, o.Failed)
		z.Failed = *o.Failed
	}
	if o.Skipped != nil && z.Skipped != *o.Skipped {
		z.LogChangeValue("skipped", z.Skipped, o.Skipped)
		z.Skipped = *o.Skipped
	}
	if o.Errors != nil {
		z.LogChangeValue("errors", z.Errors, o.Errors)
		z.Errors = *o.Errors
	}
	if o.StartedAt != nil {
		z.LogChangeValue("started_at", z.StartedAt, o.StartedAt)
		z.StartedAt = o.StartedAt
	}
	if o.FinishedAt != nil {
		z.LogChangeValue("finished_at", z.FinishedAt, o.FinishedAt)
		z.FinishedAt = o.FinishedAt
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *ImportTaskBasic) MetaAddKVs(args ...any) *ImportTaskBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *ImportTaskSet) MetaAddKVs(args ...any) *ImportTaskSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of ImportFailure 导入失败明细
const (
	ImportFailureLabel = "importFailure"
	ImportFailureTypID = "corpusImportFailure"
)

// ImportFailure 导入失败明细
type ImportFailure struct {
	// 行号（表头为第 1 行，数据从第 2 行起）
	Line int `extensions:"x-order=A" form:"line" json:"line"`
	// 该行标题
	Title string `extensions:"x-order=B" form:"title" json:"title"`
	// 失败原因
	Reason string `extensions:"x-order=C" form:"reason" json:"reason"`
	// 明细类型（失败 / 跳过），历史数据缺省表示未分类
	//  * `failed` - 失败
	//  * `skipped` - 跳过
	Kind ImportFailureKind `enums:"failed,skipped" extensions:"x-order=D" json:"kind,omitempty" swaggertype:"string"`
} // @name corpusImportFailure

type ImportFailures []ImportFailure
