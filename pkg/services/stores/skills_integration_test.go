//go:build integration

package stores

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cupogo/andvari/models/oid"
	auth "github.com/liut/simpauth"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/models/skills"
)

func TestIntegration_SkillVisibility(t *testing.T) {
	sto := Sgt()
	pid := os.Getpid()
	userA := auth.User{OID: oid.NewID(oid.OtAccount).String(), UID: "skill-a", Name: "Skill A"}
	userB := auth.User{OID: oid.NewID(oid.OtAccount).String(), UID: "skill-b", Name: "Skill B"}

	names := []string{
		fmt.Sprintf("a-none-%d", pid),
		fmt.Sprintf("a-wecom-%d", pid),
		fmt.Sprintf("b-web-%d", pid),
	}
	skillsList := []skills.SkillBasic{
		{Name: names[0], Description: "A private", Content: "---\nname: " + names[0] + "\ndescription: A private\n---\nbody",
			Channel: skills.ChannelNone, Owner: oid.Cast(userA.OID)},
		{Name: names[1], Description: "A wecom", Content: "---\nname: " + names[1] + "\ndescription: A wecom\n---\nbody",
			Channel: skills.ChannelWecom, Owner: oid.Cast(userA.OID)},
		{Name: names[2], Description: "B web", Content: "---\nname: " + names[2] + "\ndescription: B web\n---\nbody",
			Channel: skills.ChannelWeb, Owner: oid.Cast(userB.OID)},
	}
	for _, in := range skillsList {
		if _, err := sto.Skill().CreateSkill(context.Background(), in); err != nil {
			t.Fatalf("CreateSkill(%s) failed: %v", in.Name, err)
		}
	}
	t.Cleanup(func() {
		for _, in := range skillsList {
			if obj, err := sto.Skill().GetSkill(context.Background(), in.Name); err == nil && obj != nil {
				_ = sto.Skill().DeleteSkill(context.Background(), obj.ID.String())
			}
		}
	})

	ctxA := auth.ContextWithUser(mcps.ContextWithChannel(context.Background(), "wecom"), &userA)
	ctxB := auth.ContextWithUser(mcps.ContextWithChannel(context.Background(), "wecom"), &userB)

	// A 在 wecom 可见：自己的未投放 + 自己的 wecom 投放；看不到 B 的 web 投放
	listA, _, err := sto.Skill().ListVisibleMetadata(ctxA, &SkillSpec{})
	if err != nil {
		t.Fatalf("ListVisibleMetadata A failed: %v", err)
	}
	if got := countSkills(listA, names); got != 2 {
		t.Errorf("A visible count = %d, want 2 (%v)", got, names)
	}
	// 列表不含 content
	for _, sk := range listA {
		if sk.Content != "" {
			t.Errorf("metadata list leaked content for %s", sk.Name)
		}
	}

	// B 在 wecom 可见：A 投放的 wecom + 自己的 web；看不到 A 的未投放（Covers AE4）
	listB, _, err := sto.Skill().ListVisibleMetadata(ctxB, &SkillSpec{})
	if err != nil {
		t.Fatalf("ListVisibleMetadata B failed: %v", err)
	}
	if got := countSkills(listB, names); got != 2 {
		t.Errorf("B visible count = %d, want 2", got)
	}
	for _, sk := range listB {
		if sk.Name == names[0] {
			t.Errorf("B should not see A's unpublished skill %s", names[0])
		}
	}

	// LoadForName 越权按 not found（不泄露存在性）
	if _, err := sto.Skill().LoadForName(ctxB, names[0]); !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("B LoadForName(A private) err = %v, want ErrSkillNotFound", err)
	}
	// 投放的 wecom 对频道内其他用户可见
	if _, err := sto.Skill().LoadForName(ctxB, names[1]); err != nil {
		t.Errorf("B LoadForName(A wecom) err = %v, want nil", err)
	}
	// 自己的可见，且含全文
	got, err := sto.Skill().LoadForName(ctxA, names[1])
	if err != nil {
		t.Fatalf("A LoadForName failed: %v", err)
	}
	if got.Content == "" {
		t.Error("LoadForName should return full content")
	}

	// ListVisibleMetadata 强制可见性：即便 spec.VisibleOnly 传 false 也不会绕过
	specB := &SkillSpec{}
	listB2, _, err := sto.Skill().ListVisibleMetadata(ctxB, specB)
	if err != nil {
		t.Fatalf("ListVisibleMetadata B2 failed: %v", err)
	}
	if got := countSkills(listB2, names); got != 2 {
		t.Errorf("B2 visible count = %d, want 2", got)
	}

	// 管理端 ListSkill 不设 VisibleOnly：不套可见性，可见全部（含未投放与跨频道）
	listAll, _, err := sto.Skill().ListSkill(ctxB, &SkillSpec{})
	if err != nil {
		t.Fatalf("ListSkill failed: %v", err)
	}
	if got := countSkills(listAll, names); got != 3 {
		t.Errorf("admin list count = %d, want 3", got)
	}
}

func TestIntegration_SkillTopRecent(t *testing.T) {
	sto := Sgt()
	pid := os.Getpid()
	owner := oid.NewID(oid.OtAccount).String()
	user := auth.User{OID: owner, UID: "skill-top", Name: "Skill Top"}
	ctx := auth.ContextWithUser(context.Background(), &user)

	var created []string
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("top-%d-%d", i, pid)
		created = append(created, name)
		if _, err := sto.Skill().CreateSkill(ctx, skills.SkillBasic{
			Name: name, Description: name, Content: "body",
			Channel: skills.ChannelNone, Owner: oid.Cast(owner),
		}); err != nil {
			t.Fatalf("CreateSkill(%s) failed: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for _, name := range created {
			if obj, err := sto.Skill().GetSkill(context.Background(), name); err == nil && obj != nil {
				_ = sto.Skill().DeleteSkill(context.Background(), obj.ID.String())
			}
		}
	})

	data, err := sto.Skill().TopRecent(ctx, 3)
	if err != nil {
		t.Fatalf("TopRecent failed: %v", err)
	}
	if len(data) != 3 {
		t.Errorf("TopRecent len = %d, want 3", len(data))
	}
	if data[0].Name != created[4] {
		t.Errorf("TopRecent[0] = %s, want newest %s", data[0].Name, created[4])
	}
}

func countSkills(data skills.Skills, names []string) int {
	n := 0
	for _, sk := range data {
		for _, name := range names {
			if sk.Name == name {
				n++
			}
		}
	}
	return n
}

// TestIntegration_SkillFrontmatterFields 覆盖元数据列镜像 SKILL.md frontmatter 的不变量：
// 创建时派生、读回后一致、改正文后重新派生、未改正文时保留。
func TestIntegration_SkillFrontmatterFields(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	name := fmt.Sprintf("fm-%d", os.Getpid())
	owner := oid.Cast(oid.NewID(oid.OtAccount).String())
	content := `---
name: ` + name + `
description: Frontmatter fields
version: 1.2.3
author: [Alice, Bob]
license: MIT
platforms: [linux, windows]
metadata:
  hermes:
    category: research
    homepage: https://example.com/skill
    related_skills: [pdf, arxiv, pdf]
---

body
`
	obj, err := sto.Skill().CreateSkillWithFiles(ctx, skills.SkillBasic{
		Name: name, Description: "Frontmatter fields", Content: content,
		Channel: skills.ChannelWeb, Owner: owner,
	}, map[string]string{"scripts/run.sh": "echo hi"})
	if err != nil {
		t.Fatalf("CreateSkillWithFiles failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sto.Skill().DeleteSkill(context.Background(), obj.ID.String())
	})
	assertSkillMeta(t, &obj.SkillBasic, "create")

	got, err := sto.Skill().LoadForName(ctx, name)
	if err != nil {
		t.Fatalf("LoadForName failed: %v", err)
	}
	assertSkillMeta(t, &got.SkillBasic, "load")

	// 改正文后重新派生：新正文里缺失的字段被清空
	updated := `---
name: ` + name + `
description: Frontmatter fields
version: 2.0.0
license: Apache-2.0
platforms: [macos]
metadata:
  hermes:
    category: productivity
---

new body
`
	if err := sto.Skill().UpdateSkillWithFiles(ctx, obj.ID.String(), skills.SkillSet{Content: &updated}, nil); err != nil {
		t.Fatalf("UpdateSkillWithFiles failed: %v", err)
	}
	got, err = sto.Skill().LoadForName(ctx, name)
	if err != nil {
		t.Fatalf("LoadForName after update failed: %v", err)
	}
	if got.Version != "2.0.0" || got.License != "Apache-2.0" || got.Category != "productivity" {
		t.Errorf("update should re-derive fields: %+v", got.SkillBasic)
	}
	if got.Author != "" || got.Homepage != "" || len(got.RelatedSkills) != 0 {
		t.Errorf("fields dropped from frontmatter should be cleared: %+v", got.SkillBasic)
	}
	if got.Platform != skills.PlatformMacos {
		t.Errorf("platform after update = %v, want macos", got.Platform)
	}

	// 未改正文的更新：元数据列沿用库中正文的解析结果
	desc := "Frontmatter fields updated"
	if err := sto.Skill().UpdateSkillWithFiles(ctx, obj.ID.String(), skills.SkillSet{Description: &desc}, nil); err != nil {
		t.Fatalf("UpdateSkillWithFiles(desc) failed: %v", err)
	}
	got, err = sto.Skill().LoadForName(ctx, name)
	if err != nil {
		t.Fatalf("LoadForName after desc update failed: %v", err)
	}
	if got.Version != "2.0.0" || got.Category != "productivity" || got.Platform != skills.PlatformMacos {
		t.Errorf("meta columns should survive content-untouched update: %+v", got.SkillBasic)
	}
}

// TestIntegration_SkillImportMD 覆盖单文件导入：frontmatter 为权威、归属与频道语义、
// 同名不覆盖，以及校验失败不落库。
func TestIntegration_SkillImportMD(t *testing.T) {
	sto := Sgt()
	ctx := context.Background()
	pid := os.Getpid()
	name := fmt.Sprintf("imp-%d", pid)
	owner := oid.Cast(oid.NewID(oid.OtAccount).String())
	content := `---
name: ` + name + `
description: Imported skill
version: 2.1.0
platforms: [linux]
metadata:
  hermes:
    category: research
---

body
`
	obj, err := sto.Skill().ImportSkillMD(ctx, content, SkillImportOptions{Owner: owner, Channel: skills.ChannelWeb})
	if err != nil {
		t.Fatalf("ImportSkillMD failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sto.Skill().DeleteSkill(context.Background(), obj.ID.String())
	})
	if obj.Name != name || obj.Description != "Imported skill" {
		t.Errorf("name/description should come from frontmatter: %+v", obj.SkillBasic)
	}
	if obj.Version != "2.1.0" || obj.Platform != skills.PlatformLinux || obj.Category != "research" {
		t.Errorf("meta columns not derived: %+v", obj.SkillBasic)
	}
	if obj.Owner != owner || obj.Channel != skills.ChannelWeb {
		t.Errorf("owner/channel not honored: owner=%v channel=%v", obj.Owner, obj.Channel)
	}

	// 同名再导入被拒绝，且既有行不被改动
	if _, err := sto.Skill().ImportSkillMD(ctx, content, SkillImportOptions{Owner: owner}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate import err = %v, want ErrDuplicate", err)
	}
	got, err := sto.Skill().LoadForName(ctx, name)
	if err != nil {
		t.Fatalf("LoadForName failed: %v", err)
	}
	if got.Version != "2.1.0" || got.Channel != skills.ChannelWeb {
		t.Errorf("existing skill mutated by rejected import: %+v", got.SkillBasic)
	}

	// 未指定归属时取上下文用户，且默认未投放
	user := auth.User{OID: owner.String(), UID: "imp-user", Name: "Imp"}
	ctxUser := auth.ContextWithUser(context.Background(), &user)
	ctxName := name + "-ctx"
	ctxObj, err := sto.Skill().ImportSkillMD(ctxUser,
		"---\nname: "+ctxName+"\ndescription: Imported by user\n---\nbody", SkillImportOptions{})
	if err != nil {
		t.Fatalf("ImportSkillMD with user context failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sto.Skill().DeleteSkill(context.Background(), ctxObj.ID.String())
	})
	if ctxObj.Owner != owner || ctxObj.Channel != skills.ChannelNone {
		t.Errorf("context user import: owner=%v channel=%v", ctxObj.Owner, ctxObj.Channel)
	}

	// 既无归属也无上下文用户 → 明确报错
	if _, err := sto.Skill().ImportSkillMD(context.Background(),
		"---\nname: "+name+"-noowner\ndescription: no owner\n---\nbody",
		SkillImportOptions{}); !errors.Is(err, ErrSkillNeedOwner) {
		t.Errorf("missing owner err = %v, want ErrSkillNeedOwner", err)
	}

	// 校验失败不落库
	for label, bad := range map[string]string{
		"no frontmatter": "plain text",
		"bad name":       "---\nname: BadName\ndescription: d\n---\nbody",
		"long version":   "---\nname: " + name + "-long\ndescription: d\nversion: " + strings.Repeat("9", skills.MaxVersionLen+1) + "\n---\nbody",
	} {
		if _, err := sto.Skill().ImportSkillMD(ctx, bad, SkillImportOptions{Owner: owner}); err == nil {
			t.Errorf("%s: import should fail", label)
		}
	}
	if _, err := sto.Skill().GetSkill(ctx, name+"-long"); err == nil {
		t.Error("failed import should not persist a skill")
	}
}

func assertSkillMeta(t *testing.T, got *skills.SkillBasic, stage string) {
	t.Helper()
	if got.Version != "1.2.3" || got.Author != "Alice, Bob" || got.License != "MIT" {
		t.Errorf("%s scalars = %+v", stage, got)
	}
	if got.Platform != (skills.PlatformLinux | skills.PlatformWindows) {
		t.Errorf("%s platform = %v", stage, got.Platform)
	}
	if got.Category != "research" || got.Homepage != "https://example.com/skill" {
		t.Errorf("%s nested = %+v", stage, got)
	}
	if len(got.RelatedSkills) != 2 || got.RelatedSkills[0] != "pdf" || got.RelatedSkills[1] != "arxiv" {
		t.Errorf("%s relatedSkills = %v", stage, got.RelatedSkills)
	}
}
