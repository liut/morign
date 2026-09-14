package skills

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cupogo/andvari/models/oid"
	"gopkg.in/yaml.v3"
)

var (
	// 开放标准约束：小写字母数字连字符，1-64 字符
	nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

	ErrInvalidName     = errors.New("invalid skill name")
	ErrDescriptionLong = errors.New("description too long")
	ErrFrontmatterMiss = errors.New("missing skill frontmatter")
	ErrNameMismatch    = errors.New("frontmatter name mismatch")
	ErrMetaTooLong     = errors.New("skill frontmatter field too long")
)

// MaxDescriptionLen 描述的内存层软上限。DB 列已放宽为 text 不再硬限，
// 这里保留 500 是为了避免模型层无任何长度校验。其余元数据字段的上限仍与 DB 列宽一致。
const (
	MaxDescriptionLen = 500
	MaxVersionLen     = 32
	MaxAuthorLen      = 128
	MaxLicenseLen     = 32
	MaxCategoryLen    = 32
	MaxHomepageLen    = 255
)

// ValidName 校验开放标准要求的 name 格式
func ValidName(name string) bool {
	return len(name) > 0 && len(name) <= 64 && nameRe.MatchString(name)
}

// ValidDescription 校验描述长度不超过内存层软上限 MaxDescriptionLen（按字符计）。
// DB 列已为 text，本函数不保证与列宽一致。
func ValidDescription(s string) bool {
	return utf8.RuneCountInString(s) <= MaxDescriptionLen
}

// Matches 判断位掩码是否包含指定频道名（web/wecom/feishu），None（0）表示未投放，
// 不匹配任何频道（可见性由 owner 规则承担）。空频道名按 web 处理（HTTP 请求无频道上下文）。
func (c Channel) Matches(channel string) bool {
	if c == ChannelNone {
		return false
	}
	var bit Channel
	switch channel {
	case "wecom":
		bit = ChannelWecom
	case "feishu":
		bit = ChannelFeishu
	default:
		bit = ChannelWeb
	}
	return c&bit != 0
}

// ParseChannels 解析逗号分隔的频道名（web/wecom/feishu），返回位掩码。
// 空项忽略；遇到未知名字返回错误，不静默丢弃。
func ParseChannels(s string) (Channel, error) {
	var out Channel
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var c Channel
		if err := c.Decode(name); err != nil {
			return ChannelNone, err
		}
		out |= c
	}
	return out, nil
}

// SkillMeta 是 SKILL.md frontmatter 中的技能元数据，字段名沿用开放 Agent Skills 标准。
// 除 Name / Description 外全部可空，未知字段忽略。
type SkillMeta struct {
	Name          string
	Description   string
	Version       string
	Author        string
	License       string
	Platforms     Platform
	Category      string
	Homepage      string
	RelatedSkills []string
}

// Frontmatter 解析 SKILL.md 开头的 YAML frontmatter，返回技能元数据与去掉
// frontmatter 后的正文。无 frontmatter 或 YAML 不可解析时返回 ok=false。
func Frontmatter(content string) (meta SkillMeta, body string, ok bool) {
	yamlText, body, ok := cutFrontmatter(content)
	if !ok {
		return SkillMeta{}, "", false
	}
	var raw rawFrontmatter
	if err := yaml.Unmarshal([]byte(yamlText), &raw); err != nil {
		return SkillMeta{}, "", false
	}
	return raw.meta(), body, true
}

// rawFrontmatter 是 frontmatter 的解码形态；开放标准的两处位置差异在此收敛：
// version/author/license/platforms 在顶层，category/homepage/related_skills
// 嵌在 metadata.hermes 下。
type rawFrontmatter struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Version     string       `yaml:"version"`
	Author      stringOrList `yaml:"author"`
	License     string       `yaml:"license"`
	Platforms   stringOrList `yaml:"platforms"`
	Metadata    struct {
		Hermes struct {
			Category      string   `yaml:"category"`
			Homepage      string   `yaml:"homepage"`
			RelatedSkills []string `yaml:"related_skills"`
		} `yaml:"hermes"`
	} `yaml:"metadata"`
}

func (z rawFrontmatter) meta() SkillMeta {
	return SkillMeta{
		Name:          strings.TrimSpace(z.Name),
		Description:   strings.TrimSpace(z.Description),
		Version:       strings.TrimSpace(z.Version),
		Author:        strings.Join(z.Author, ", "),
		License:       strings.TrimSpace(z.License),
		Platforms:     parsePlatforms(z.Platforms),
		Category:      strings.TrimSpace(z.Metadata.Hermes.Category),
		Homepage:      strings.TrimSpace(z.Metadata.Hermes.Homepage),
		RelatedSkills: trimList(z.Metadata.Hermes.RelatedSkills),
	}
}

// stringOrList 接受 YAML 标量或序列，两者在语料里都出现过
// （`author: Hermes Agent` 与 `author: [a, b]`）。
type stringOrList []string

func (z *stringOrList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		*z = stringOrList{s}
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*z = stringOrList(list)
	}
	return nil
}

// parsePlatforms 把 platforms 列表映射为位掩码；未知平台名忽略。
func parsePlatforms(names []string) Platform {
	var out Platform
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "linux":
			out |= PlatformLinux
		case "macos":
			out |= PlatformMacos
		case "windows":
			out |= PlatformWindows
		}
	}
	return out
}

// trimList 去除空白与空项并去重，保持原顺序。
func trimList(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ValidateFrontmatter 校验内容中的 frontmatter：存在且 name/description 非空，
// name 与记录名一致且符合格式，其余元数据字段不超过 DB 列宽。
func ValidateFrontmatter(content, name, desc string) error {
	meta, _, ok := Frontmatter(content)
	if !ok {
		return ErrFrontmatterMiss
	}
	if meta.Name == "" || meta.Description == "" {
		return ErrFrontmatterMiss
	}
	if meta.Name != name {
		return ErrNameMismatch
	}
	return meta.Validate()
}

// Validate 校验元数据字段长度是否超出 DB 列宽（按字符计，与 varchar 语义一致）。
func (m SkillMeta) Validate() error {
	for _, f := range []struct {
		name  string
		value string
		max   int
	}{
		{"version", m.Version, MaxVersionLen},
		{"author", m.Author, MaxAuthorLen},
		{"license", m.License, MaxLicenseLen},
		{"category", m.Category, MaxCategoryLen},
		{"homepage", m.Homepage, MaxHomepageLen},
	} {
		if utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%w: %s", ErrMetaTooLong, f.name)
		}
	}
	return nil
}

// ApplyTo 把元数据写入基础字段，使 7 个列始终镜像 SKILL.md frontmatter。
func (m SkillMeta) ApplyTo(b *SkillBasic) {
	b.Version = m.Version
	b.Author = m.Author
	b.License = m.License
	b.Platform = m.Platforms
	b.Category = m.Category
	b.Homepage = m.Homepage
	b.RelatedSkills = m.RelatedSkills
}

// ApplySet 把元数据写入部分更新集，覆盖同名 7 个字段。
func (m SkillMeta) ApplySet(s *SkillSet) {
	s.Version = &m.Version
	s.Author = &m.Author
	s.License = &m.License
	s.Platform = &m.Platforms
	s.Category = &m.Category
	s.Homepage = &m.Homepage
	s.RelatedSkills = &m.RelatedSkills
}

// cutFrontmatter \u5207\u51fa SKILL.md \u7684 YAML frontmatter \u4e0e\u5269\u4f59\u6b63\u6587\uff1a\u8fd4\u56de
// (yaml \u5185\u90e8, \n--- \u4e4b\u540e\u7684\u5269\u4f59\u539f\u6587, true)\u3002\u5269\u4f59\u539f\u6587\u4fdd\u7559\u539f\u6587\u539f\u8c8c\uff08\u542b\u7d27\u8d34
// \n--- \u540e\u7684\u6362\u884c\uff09\uff0c\u4e0b\u6e38\u9700\u8981\u65f6\u53ef\u62fc\u56de\u5b8c\u6574 SKILL.md\u3002
func cutFrontmatter(content string) (yamlText, body string, ok bool) {
	s := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(s, "---") {
		return "", "", false
	}
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) < 2 {
		return "", "", false
	}
	rest := lines[1]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len("\n---"):], true
}

func (z *SkillBasic) GetOwnerID() oid.OID {
	return z.Owner
}

func (z *SkillBasic) SetOwnerID(id any) bool {
	if v := oid.Cast(id); v.Valid() {
		z.Owner = v
		return true
	}
	return false
}

func (z *SkillBasic) OwnerEmpty() bool {
	return false
}
