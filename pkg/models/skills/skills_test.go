package skills

import (
	"errors"
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"invoice", true},
		{"pdf-processing", true},
		{"a1-b2", true},
		{"", false},
		{"PDF", false},
		{"-pdf", false},
		{"pdf-", false},
		{"pdf--x", false},
		{"pdf processing", false},
		{"pdf_processing", false},
		{string(make([]byte, 65)), false},
	}
	for _, c := range cases {
		if got := ValidName(c.name); got != c.want {
			t.Errorf("ValidName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidDescription(t *testing.T) {
	ok := strings.Repeat("a", MaxDescriptionLen)
	if !ValidDescription(ok) {
		t.Errorf("length %d should be valid", MaxDescriptionLen)
	}
	if ValidDescription(ok + "a") {
		t.Error("length over limit should be invalid")
	}
	// 中文字符按字符计，不按字节
	cn := strings.Repeat("技", MaxDescriptionLen)
	if !ValidDescription(cn) {
		t.Error("multi-byte chars should count by rune")
	}
	if ValidDescription(cn + "技") {
		t.Error("multi-byte over limit should be invalid")
	}
}

func TestChannelMatches(t *testing.T) {
	if ChannelNone.Matches("wecom") {
		t.Error("ChannelNone should not match any channel")
	}
	if ChannelNone.Matches("") {
		t.Error("ChannelNone should not match empty channel")
	}
	if !ChannelWecom.Matches("wecom") {
		t.Error("ChannelWecom should match wecom")
	}
	if ChannelWecom.Matches("feishu") {
		t.Error("ChannelWecom should not match feishu")
	}
	if ChannelWecom.Matches("") {
		t.Error("ChannelWecom should not match web")
	}
	if !ChannelWeb.Matches("") {
		t.Error("empty channel should map to web")
	}
	if (ChannelWeb | ChannelFeishu).Matches("feishu") == false {
		t.Error("combined mask should match feishu")
	}
	if (ChannelWeb | ChannelFeishu).Matches("wecom") {
		t.Error("combined mask should not match wecom")
	}
}

func TestFrontmatter(t *testing.T) {
	minimal := "---\nname: invoice\ndescription: 处理发票\n---\n\n正文"
	meta, _, ok := Frontmatter(minimal)
	if !ok || meta.Name != "invoice" || meta.Description != "处理发票" {
		t.Fatalf("Frontmatter = (%+v, %v)", meta, ok)
	}
	if meta.Version != "" || meta.Author != "" || meta.Platforms != PlatformNone || len(meta.RelatedSkills) != 0 {
		t.Errorf("absent optional fields should stay zero: %+v", meta)
	}
	if _, _, ok := Frontmatter("plain markdown"); ok {
		t.Error("plain markdown should not have frontmatter")
	}
	if _, _, ok := Frontmatter("---\nname: [a\n---\nbody"); ok {
		t.Error("broken yaml should not parse")
	}
}

func TestFrontmatterMeta(t *testing.T) {
	full := `---
name: arxiv
description: Search arXiv papers.
version: 1.0.0
author: Hermes Agent
license: MIT
platforms: [linux, macos]
tags: [Research, Papers]
metadata:
  hermes:
    category: research
    homepage: https://arxiv.org
    related_skills: [pdf, arxiv, pdf]
---

# arXiv
`
	meta, _, ok := Frontmatter(full)
	if !ok {
		t.Fatal("full frontmatter should parse")
	}
	if meta.Version != "1.0.0" || meta.Author != "Hermes Agent" || meta.License != "MIT" {
		t.Errorf("scalar fields = %+v", meta)
	}
	if meta.Platforms != (PlatformLinux | PlatformMacos) {
		t.Errorf("platforms = %v, want linux|macos", meta.Platforms)
	}
	if meta.Category != "research" || meta.Homepage != "https://arxiv.org" {
		t.Errorf("nested metadata fields = %+v", meta)
	}
	if len(meta.RelatedSkills) != 2 || meta.RelatedSkills[0] != "pdf" || meta.RelatedSkills[1] != "arxiv" {
		t.Errorf("related skills should be trimmed and deduped: %v", meta.RelatedSkills)
	}

	// author 的序列写法与未知平台名，两者在语料里都存在
	listAuthor := `---
name: comfyui
description: ComfyUI.
author: [kshitijk4poor, alt-glitch]
platforms: [macos, plan9]
---
body
`
	meta, _, ok = Frontmatter(listAuthor)
	if !ok || meta.Author != "kshitijk4poor, alt-glitch" {
		t.Errorf("author list = (%q, %v), want joined", meta.Author, ok)
	}
	if meta.Platforms != PlatformMacos {
		t.Errorf("unknown platform should be ignored, got %v", meta.Platforms)
	}
}

func TestValidateFrontmatter(t *testing.T) {
	good := "---\nname: invoice\ndescription: 处理发票\n---\n\n正文"
	if err := ValidateFrontmatter(good, "invoice", "处理发票"); err != nil {
		t.Errorf("ValidateFrontmatter(good) = %v", err)
	}
	if err := ValidateFrontmatter("plain", "invoice", "d"); err != ErrFrontmatterMiss {
		t.Errorf("missing frontmatter err = %v", err)
	}
	if err := ValidateFrontmatter("---\nname: other\ndescription: d\n---", "invoice", "d"); err != ErrNameMismatch {
		t.Errorf("mismatch err = %v", err)
	}
	long := "---\nname: invoice\ndescription: d\nversion: " + strings.Repeat("9", MaxVersionLen+1) + "\n---\nbody"
	if err := ValidateFrontmatter(long, "invoice", "d"); !errors.Is(err, ErrMetaTooLong) {
		t.Errorf("over-long version err = %v, want ErrMetaTooLong", err)
	}
	atLimit := "---\nname: invoice\ndescription: d\nhomepage: " + strings.Repeat("u", MaxHomepageLen) + "\n---\nbody"
	if err := ValidateFrontmatter(atLimit, "invoice", "d"); err != nil {
		t.Errorf("homepage at limit should pass, got %v", err)
	}
}

func TestSkillMetaApply(t *testing.T) {
	meta := SkillMeta{
		Version: "1.0.0", Author: "Teknium", License: "MIT",
		Platforms: PlatformLinux | PlatformWindows, Category: "research",
		Homepage: "https://example.com", RelatedSkills: []string{"pdf"},
	}
	var basic SkillBasic
	meta.ApplyTo(&basic)
	if basic.Version != "1.0.0" || basic.Author != "Teknium" || basic.License != "MIT" {
		t.Errorf("ApplyTo scalars = %+v", basic)
	}
	if basic.Platform != (PlatformLinux|PlatformWindows) || basic.Category != "research" || basic.Homepage != "https://example.com" {
		t.Errorf("ApplyTo meta = %+v", basic)
	}
	if len(basic.RelatedSkills) != 1 || basic.RelatedSkills[0] != "pdf" {
		t.Errorf("ApplyTo related = %v", basic.RelatedSkills)
	}

	var set SkillSet
	meta.ApplySet(&set)
	if set.Version == nil || *set.Version != "1.0.0" {
		t.Errorf("ApplySet version = %v", set.Version)
	}
	if set.Platform == nil || *set.Platform != (PlatformLinux|PlatformWindows) {
		t.Errorf("ApplySet platform = %v", set.Platform)
	}
	if set.RelatedSkills == nil || len(*set.RelatedSkills) != 1 {
		t.Errorf("ApplySet related = %v", set.RelatedSkills)
	}
}

func TestParseChannels(t *testing.T) {
	cases := []struct {
		in   string
		want Channel
		err  bool
	}{
		{"web", ChannelWeb, false},
		{"web,wecom", ChannelWeb | ChannelWecom, false},
		{" web , feishu ", ChannelWeb | ChannelFeishu, false},
		{"", ChannelNone, false},
		{"web,,wecom", ChannelWeb | ChannelWecom, false},
		{"none", ChannelNone, false},
		{"web,sms", ChannelNone, true},
	}
	for _, c := range cases {
		got, err := ParseChannels(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseChannels(%q) should fail", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseChannels(%q) = (%v, %v), want %v", c.in, got, err, c.want)
		}
	}
}
