package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/stores"
)

const maxImportUploadSize = 10 << 20 // 10 MiB

func init() {
	regHI(true, "POST", "/corpus/imports", "corpus-imports-post", func(a *api) http.HandlerFunc {
		return a.postCorpusImport
	})
	regHI(true, "GET", "/corpus/imports", "corpus-imports-get", func(a *api) http.HandlerFunc {
		return a.getCorpusImports
	})
	regHI(true, "GET", "/corpus/imports/:id", "corpus-imports-id-get", func(a *api) http.HandlerFunc {
		return a.getCorpusImport
	})
}

// corpusImportTaskView 任务视图：剔除 CSV 原文；列表复用（Errors 置空即不输出）
// @name corpusImportTaskView
type corpusImportTaskView struct {
	ID         string                  `json:"id"`
	Filename   string                  `json:"filename"`
	Status     corpus.ImportTaskStatus `json:"status"`
	Total      int                     `json:"total"`
	Success    int                     `json:"success"`
	Failed     int                     `json:"failed"`
	Skipped    int                     `json:"skipped"`
	Errors     []corpus.ImportFailure  `json:"errors,omitempty"`
	CreatedAt  time.Time               `json:"createdAt"`
	UpdatedAt  *time.Time              `json:"updatedAt,omitempty"`
	StartedAt  *time.Time              `json:"startedAt,omitempty"`
	FinishedAt *time.Time              `json:"finishedAt,omitempty"`
}

func newImportTaskView(obj *corpus.ImportTask) *corpusImportTaskView {
	return &corpusImportTaskView{
		ID:         obj.StringID(),
		Filename:   obj.Filename,
		Status:     obj.Status,
		Total:      obj.Total,
		Success:    obj.Success,
		Failed:     obj.Failed,
		Skipped:    obj.Skipped,
		Errors:     obj.Errors,
		CreatedAt:  obj.CreatedAt,
		UpdatedAt:  obj.UpdatedAt,
		StartedAt:  obj.StartedAt,
		FinishedAt: obj.FinishedAt,
	}
}

// @Tags 语料 文档导入
// @Summary 上传 CSV 创建导入任务 🔑
// @Accept mpfd
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param file formData file true "CSV 文件（title,heading,content，UTF-8）"
// @Success 200 {object} Done{result=corpusImportTaskView}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 413 {object} Failure "文件过大"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/imports [post]
func (a *api) postCorpusImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUploadSize)
	file, fh, err := r.FormFile("file")
	if err != nil {
		if isTooLarge(err) {
			fail(w, r, 413, "file too large, max 10MiB")
			return
		}
		fail(w, r, 400, "missing file field")
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		if isTooLarge(err) {
			fail(w, r, 413, "file too large, max 10MiB")
			return
		}
		fail(w, r, 400, err)
		return
	}
	if !utf8.Valid(data) {
		fail(w, r, 400, "file must be UTF-8 encoded")
		return
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !validImportCSVHead(data) {
		fail(w, r, 400, "invalid csv header, expected: title,heading,content")
		return
	}

	obj, err := a.sto.Corpus().CreateImportTask(r.Context(), corpus.ImportTaskBasic{
		Filename: fh.Filename,
		Status:   corpus.ImportTaskStatusPending,
		Data:     string(data),
	})
	if err != nil {
		fail(w, r, 503, err)
		return
	}
	success(w, r, newImportTaskView(obj))
}

// @Tags 语料 文档导入
// @Summary 查询导入任务列表 🔑
// @Description <sortable>created,filename,status</sortable>
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param query query stores.CobImportTaskSpec true "Object"
// @Success 200 {object} Done{result=ResultData{data=[]corpusImportTaskView}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/imports [get]
func (a *api) getCorpusImports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var spec stores.CobImportTaskSpec
	if s := q.Get("status"); s != "" {
		if err := spec.Status.Decode(s); err != nil {
			fail(w, r, 400, err)
			return
		}
	}
	spec.Filename = q.Get("filename")
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(w, r, 400, "invalid limit")
			return
		}
		spec.Limit = n
	} else {
		spec.Limit = 20
	}
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(w, r, 400, "invalid page")
			return
		}
		spec.Page = n
	}
	spec.Sort = q.Get("sort")
	if spec.Sort == "" {
		spec.Sort = "-created"
	}
	spec.ExcludeColumn("data", "errors")

	data, total, err := a.sto.Corpus().ListImportTask(r.Context(), &spec)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	items := make([]*corpusImportTaskView, len(data))
	for i := range data {
		items[i] = newImportTaskView(&data[i])
		items[i].Errors = nil
	}
	success(w, r, dtResult(items, total))
}

// @Tags 语料 文档导入
// @Summary 获取导入任务详情 🔑
// @Accept json
// @Produce json
// @Param token header string true "登录票据凭证"
// @Param id path string true "任务ID"
// @Success 200 {object} Done{result=corpusImportTaskView}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/imports/{id} [get]
func (a *api) getCorpusImport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	obj, err := a.sto.Corpus().GetImportTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, stores.ErrNotFound) {
			fail(w, r, 404, err)
			return
		}
		if errors.Is(err, stores.ErrInvalidID) {
			fail(w, r, 400, err)
			return
		}
		fail(w, r, 503, err)
		return
	}
	success(w, r, newImportTaskView(obj))
}

func isTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func validImportCSVHead(data []byte) bool {
	rd := csv.NewReader(bytes.NewReader(data))
	rec, err := rd.Read()
	if err != nil {
		return false
	}
	return stores.ValidHead(rec)
}
