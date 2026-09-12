// This file is generated - Do Not Edit.

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liut/morign/pkg/models/corpus"
	"github.com/liut/morign/pkg/services/stores"
	binder "github.com/marcsv/go-binder/binder"
)

func init() {
	regHI(true, "GET", "/corpus/documents", "", func(a *api) http.HandlerFunc {
		return a.getCorpusDocuments
	})
	regHI(true, "GET", "/corpus/documents/{id}", "", func(a *api) http.HandlerFunc {
		return a.getCorpusDocument
	})
	regHI(true, "PUT", "/corpus/documents/{id}", "corpus-documents-id-put", func(a *api) http.HandlerFunc {
		return a.putCorpusDocument
	})
	regHI(true, "DELETE", "/corpus/documents/{id}", "corpus-documents-id-delete", func(a *api) http.HandlerFunc {
		return a.deleteCorpusDocument
	})
}

// @Tags 默认 文档生成
// @Description <sortable>id,created,updated,heading</sortable>
// @Summary 查询 文档 列表
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   query  query   stores.CobDocumentSpec  true   "Object"
// @Success 200 {object} Done{result=ResultData{data=corpus.Documents}}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/documents [get]
func (a *api) getCorpusDocuments(w http.ResponseWriter, r *http.Request) {
	var spec stores.CobDocumentSpec
	if err := queryBinder.Bind(&spec, r.URL); err != nil {
		fail(w, r, 400, err)
		return
	}

	ctx := r.Context()
	data, total, err := a.sto.Corpus().ListDocument(ctx, &spec)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	if total == 0 {
		total = len(data)
	}
	success(w, r, dtResult(data, total))
}

// @Tags 默认 文档生成
// @Summary 获取 文档 详情
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done{result=corpus.Document}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 404 {object} Failure "目标未找到"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/documents/{id} [get]
func (a *api) getCorpusDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var obj *corpus.Document
	var err error
	obj, err = a.sto.Corpus().GetDocument(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, obj)
}

// @Tags 默认 文档生成
// @ID corpus-documents-id-put
// @Summary 更新 文档 🔑
// @Accept json,mpfd
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Param   query  body   corpus.DocumentSet  true   "Object"
// @Success 200 {object} Done{result=string}
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/documents/{id} [put]
func (a *api) putCorpusDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in corpus.DocumentSet
	if err := binder.BindBody(r, &in); err != nil {
		fail(w, r, 400, err)
		return
	}

	err := a.sto.Corpus().UpdateDocument(r.Context(), id, in)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}

// @Tags 默认 文档生成
// @ID corpus-documents-id-delete
// @Summary 删除 文档 🔑
// @Accept json
// @Produce json
// @Param token    header   string  true "登录票据凭证"
// @Param   id    path   string  true   "编号"
// @Success 200 {object} Done
// @Failure 400 {object} Failure "请求或参数错误"
// @Failure 401 {object} Failure "未登录"
// @Failure 403 {object} Failure "无权限"
// @Failure 503 {object} Failure "服务端错误"
// @Router /api/corpus/documents/{id} [delete]
func (a *api) deleteCorpusDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.sto.Corpus().DeleteDocument(r.Context(), id)
	if err != nil {
		fail(w, r, 503, err)
		return
	}

	success(w, r, "ok")
}
