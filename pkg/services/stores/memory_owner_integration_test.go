//go:build integration

package stores

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"

	"github.com/cupogo/andvari/models/oid"

	"github.com/liut/morign/pkg/models/convo"
	"github.com/liut/morign/pkg/models/corpus"
)

// bucketEmbedding 按最后一个空白分隔的词分桶，让同一内容的记忆与查询落到同一向量，
// 其余向量与它们正交，召回结果因此可预期。
type bucketEmbedding struct{ mockEmbeddingClient }

func (f *bucketEmbedding) Embedding(ctx context.Context, texts []string) ([]float64, error) {
	out := make([]float64, 0, len(texts)*corpus.VectorLen)
	for _, text := range texts {
		token := text
		if fields := strings.Fields(text); len(fields) > 0 {
			token = fields[len(fields)-1]
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		vec := make([]float64, corpus.VectorLen)
		vec[int(h.Sum32())%corpus.VectorLen] = 1
		out = append(out, vec...)
	}
	return out, nil
}

func TestIntegration_MemoryOwnershipEnforced(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	ownerA, ownerB := oid.NewID(oid.OtAccount), oid.NewID(oid.OtAccount)

	mem, err := sto.Convo().CreateMemory(withAccount(ctx, ownerA), convo.MemoryBasic{
		OwnerID: ownerB,
		Key:     fmt.Sprintf("own-%d", os.Getpid()),
		Cate:    "core",
		Content: "ownership content",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	defer sto.Convo().DeleteMemory(ctx, mem.ID.String())
	if mem.OwnerID != ownerA {
		t.Errorf("owner = %v, want requester %v", mem.OwnerID, ownerA)
	}

	id := mem.ID.String()
	if _, err := sto.Convo().GetMemory(withAccount(ctx, ownerB), id); !errors.Is(err, ErrForbidden) {
		t.Errorf("detail by another user = %v, want ErrForbidden", err)
	}
	content := "hijack"
	if err := sto.Convo().UpdateMemory(withAccount(ctx, ownerB), id, convo.MemorySet{Content: &content}); !errors.Is(err, ErrForbidden) {
		t.Errorf("update by another user = %v, want ErrForbidden", err)
	}
	if err := sto.Convo().DeleteMemory(withAccount(ctx, ownerB), id); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete by another user = %v, want ErrForbidden", err)
	}
	if _, err := sto.Convo().GetMemory(withAccount(ctx, ownerA), id); err != nil {
		t.Errorf("detail by owner = %v, want nil", err)
	}
	if _, err := sto.Convo().GetMemory(withKeeper(ctx, ownerB), id); err != nil {
		t.Errorf("detail by keeper = %v, want nil", err)
	}
}

func TestIntegration_MemoryDeleteRemovesVector(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	owner := oid.NewID(oid.OtAccount)

	mem, err := sto.Convo().CreateMemory(ctx, convo.MemoryBasic{
		OwnerID: owner,
		Key:     fmt.Sprintf("vec-%d", os.Getpid()),
		Cate:    "core",
		Content: "vector cleanup content",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	if err := dbGetWithUnique(ctx, sto.db, new(corpus.DocVector), "doc_id", mem.ID); err != nil {
		t.Fatalf("memory vector missing before delete: %v", err)
	}
	if err := sto.Convo().DeleteMemory(ctx, mem.ID.String()); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if err := dbGetWithUnique(ctx, sto.db, new(corpus.DocVector), "doc_id", mem.ID); err == nil {
		t.Error("vector should be removed together with the memory")
	}
}

func TestIntegration_MemoryRecallOwnership(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	old := llmEm
	llmEm = &bucketEmbedding{mockEmbeddingClient{}}
	defer func() { llmEm = old }()

	ownerA, ownerB := oid.NewID(oid.OtAccount), oid.NewID(oid.OtAccount)
	content := fmt.Sprintf("shared-%d", os.Getpid())

	memA, err := sto.Convo().CreateMemory(ctx, convo.MemoryBasic{OwnerID: ownerA, Key: "recall-a", Cate: "core", Content: content})
	if err != nil {
		t.Fatalf("CreateMemory A: %v", err)
	}
	defer sto.Convo().DeleteMemory(ctx, memA.ID.String())
	memB, err := sto.Convo().CreateMemory(ctx, convo.MemoryBasic{OwnerID: ownerB, Key: "recall-b", Cate: "core", Content: content})
	if err != nil {
		t.Fatalf("CreateMemory B: %v", err)
	}
	defer sto.Convo().DeleteMemory(ctx, memB.ID.String())

	got, err := sto.Convo().MatchMemories(withAccount(ctx, ownerA), MatchSpec{Query: content, Limit: 20})
	if err != nil {
		t.Fatalf("MatchMemories as user: %v", err)
	}
	sawOwn := false
	for _, m := range got {
		if m.OwnerID != ownerA {
			t.Errorf("user recall leaked memory %v owned by %v", m.ID, m.OwnerID)
		}
		if m.ID == memA.ID {
			sawOwn = true
		}
	}
	if !sawOwn {
		t.Fatalf("own memory not recalled (%d results)", len(got))
	}

	keeperGot, err := sto.Convo().MatchMemories(withKeeper(ctx, ownerA), MatchSpec{Query: content, Limit: 20})
	if err != nil {
		t.Fatalf("MatchMemories as keeper: %v", err)
	}
	ids := make(map[oid.OID]bool, len(keeperGot))
	for _, m := range keeperGot {
		ids[m.ID] = true
	}
	if !ids[memA.ID] || !ids[memB.ID] {
		t.Errorf("keeper recall missing memories: got %v", ids)
	}
}
