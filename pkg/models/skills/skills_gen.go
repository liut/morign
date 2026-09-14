// This file is generated - Do Not Edit.

package skills

import (
	"fmt"

	comm "github.com/cupogo/andvari/models/comm"
	oid "github.com/cupogo/andvari/models/oid"
)

// 技能可用频道 位掩码（0=未投放，仅创建者可见）
type Channel int8

const (
	ChannelWeb    Channel = 1 << iota //   1 Web
	ChannelWecom                      //   2 企业微信
	ChannelFeishu                     //   4 飞书

	ChannelNone Channel = 0 // 无
)

func (z *Channel) Decode(s string) error {
	switch s {
	case "0", "none":
		*z = ChannelNone
	case "1", "web", "Web":
		*z = ChannelWeb
	case "2", "wecom", "Wecom":
		*z = ChannelWecom
	case "4", "feishu", "Feishu":
		*z = ChannelFeishu
	default:
		return fmt.Errorf("invalid channel: %q", s)
	}
	return nil
}
func (z *Channel) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z Channel) String() string {
	switch z {
	case ChannelNone:
		return "none"
	case ChannelWeb:
		return "web"
	case ChannelWecom:
		return "wecom"
	case ChannelFeishu:
		return "feishu"
	default:
		return fmt.Sprintf("channel %d", int8(z))
	}
}
func (z Channel) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// 技能资源文件类型
type FileKind int8

const (
	FileKindText   FileKind = 1 + iota //  1 文本
	FileKindBinary                     //  2 二进制
)

func (z *FileKind) Decode(s string) error {
	switch s {
	case "1", "text", "Text":
		*z = FileKindText
	case "2", "binary", "Binary":
		*z = FileKindBinary
	default:
		return fmt.Errorf("invalid fileKind: %q", s)
	}
	return nil
}
func (z *FileKind) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z FileKind) String() string {
	switch z {
	case FileKindText:
		return "text"
	case FileKindBinary:
		return "binary"
	default:
		return fmt.Sprintf("fileKind %d", int8(z))
	}
}
func (z FileKind) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// 技能适用平台 位掩码（0=未声明，不参与平台门控）
type Platform int8

const (
	PlatformLinux   Platform = 1 << iota //   1 Linux
	PlatformMacos                        //   2 macOS
	PlatformWindows                      //   4 Windows

	PlatformNone Platform = 0 // 未声明
)

func (z *Platform) Decode(s string) error {
	switch s {
	case "0", "none":
		*z = PlatformNone
	case "1", "linux", "Linux":
		*z = PlatformLinux
	case "2", "macos", "Macos", "macOS":
		*z = PlatformMacos
	case "4", "windows", "Windows":
		*z = PlatformWindows
	default:
		return fmt.Errorf("invalid platform: %q", s)
	}
	return nil
}
func (z *Platform) UnmarshalText(b []byte) error {
	return z.Decode(string(b))
}
func (z Platform) String() string {
	switch z {
	case PlatformNone:
		return "none"
	case PlatformLinux:
		return "linux"
	case PlatformMacos:
		return "macos"
	case PlatformWindows:
		return "windows"
	default:
		return fmt.Sprintf("platform %d", int8(z))
	}
}
func (z Platform) MarshalText() ([]byte, error) {
	return []byte(z.String()), nil
}

// consts of Skill 技能
const (
	SkillTable = "agent_skill"
	SkillAlias = "sk"
	SkillLabel = "skill"
	SkillTypID = "skillsSkill"
)

// Skill 技能
type Skill struct {
	comm.BaseModel `bun:"table:agent_skill,alias:sk" json:"-"`

	comm.DefaultModel

	SkillBasic

	comm.MetaField
} // @name skillsSkill

type SkillBasic struct {
	// 技能名 全局唯一（小写字母数字连字符）
	Name string `binding:"required" bson:"name" bun:",notnull,unique,type:name" extensions:"x-order=A" form:"name" json:"name" pg:",notnull,unique,type:name"`
	// 技能描述 做什么+何时用
	Description string `bson:"description" bun:",notnull,type:text" extensions:"x-order=B" form:"description" json:"description" pg:",notnull,type:text"`
	// 正文
	Content string `bson:"content" bun:",notnull,type:text" extensions:"x-order=C" form:"content" json:"content,omitempty" pg:",notnull,type:text"`
	// 可用频道 位掩码（0=未投放，仅创建者可见）
	//  * `web`
	//  * `wecom` - 企业微信
	//  * `feishu` - 飞书
	Channel Channel `bson:"channel" bun:",notnull,type:smallint,default:0" enums:"web,wecom,feishu" extensions:"x-order=D" json:"channel" pg:",notnull,type:smallint,default:0" swaggertype:"string"`
	// 创建者 uid
	Owner oid.OID `bun:"owner,notnull,type:bigint" extensions:"x-order=E" json:"owner" pg:"owner,notnull,type:bigint" swaggertype:"string"`
	// 版本 取自 SKILL.md frontmatter version
	Version string `bson:"version" bun:",notnull,type:varchar(32),default:''" extensions:"x-order=F" form:"version" json:"version,omitempty" pg:",notnull,type:varchar(32),default:''"`
	// 作者 取自 SKILL.md frontmatter author
	Author string `bson:"author" bun:",notnull,type:varchar(128),default:''" extensions:"x-order=G" form:"author" json:"author,omitempty" pg:",notnull,type:varchar(128),default:''"`
	// 许可 取自 SKILL.md frontmatter license
	License string `bson:"license" bun:",notnull,type:varchar(32),default:''" extensions:"x-order=H" form:"license" json:"license,omitempty" pg:",notnull,type:varchar(32),default:''"`
	// 适用平台 位掩码（0=未声明，不参与平台门控）取自 frontmatter platforms
	//  * `linux`
	//  * `macos` - macOS
	//  * `windows`
	Platform Platform `bson:"platform" bun:",notnull,type:smallint,default:0" enums:"linux,macos,windows" extensions:"x-order=I" json:"platform" pg:",notnull,type:smallint,default:0" swaggertype:"string"`
	// 分类 取自 frontmatter metadata.hermes.category
	Category string `bson:"category" bun:",notnull,type:varchar(32),default:''" extensions:"x-order=J" form:"category" json:"category,omitempty" pg:",notnull,type:varchar(32),default:''"`
	// 主页 取自 frontmatter metadata.hermes.homepage
	Homepage string `bson:"homepage" bun:",notnull,type:varchar(255),default:''" extensions:"x-order=K" form:"homepage" json:"homepage,omitempty" pg:",notnull,type:varchar(255),default:''"`
	// 关联技能名 取自 frontmatter metadata.hermes.related_skills
	RelatedSkills []string `bson:"relatedSkills" bun:",notnull,type:jsonb,default:'[]'" extensions:"x-order=L" json:"relatedSkills,omitempty" pg:",notnull,type:jsonb,default:'[]'"`
	// for meta update
	MetaDiff *comm.MetaDiff `bson:"-" bun:"-" json:"metaUp,omitempty" pg:"-" swaggerignore:"true"`
} // @name skillsSkillBasic

type Skills []Skill

// Creating function call to it's inner fields defined hooks
func (z *Skill) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtFile))
	}

	return z.DefaultModel.Creating()
}
func NewSkillWithBasic(in SkillBasic) *Skill {
	obj := &Skill{
		SkillBasic: in,
	}
	_ = obj.MetaUp(in.MetaDiff)
	return obj
}
func NewSkillWithID(id any) *Skill {
	obj := new(Skill)
	_ = obj.SetID(id)
	return obj
}
func (_ *Skill) IdentityLabel() string { return SkillLabel }
func (_ *Skill) IdentityModel() string { return SkillTypID }
func (_ *Skill) IdentityTable() string { return SkillTable }
func (_ *Skill) IdentityAlias() string { return SkillAlias }

type SkillSet struct {
	// 技能名 全局唯一（小写字母数字连字符）
	Name *string `extensions:"x-order=A" json:"name"`
	// 技能描述 做什么+何时用
	Description *string `extensions:"x-order=B" json:"description"`
	// 正文
	Content *string `extensions:"x-order=C" form:"content" json:"content,omitempty"`
	// 可用频道 位掩码（0=未投放，仅创建者可见）
	//  * `web`
	//  * `wecom` - 企业微信
	//  * `feishu` - 飞书
	Channel *Channel `enums:"web,wecom,feishu" extensions:"x-order=D" json:"channel" swaggertype:"string"`
	// 版本 取自 SKILL.md frontmatter version
	Version *string `extensions:"x-order=E" form:"version" json:"version,omitempty"`
	// 作者 取自 SKILL.md frontmatter author
	Author *string `extensions:"x-order=F" form:"author" json:"author,omitempty"`
	// 许可 取自 SKILL.md frontmatter license
	License *string `extensions:"x-order=G" form:"license" json:"license,omitempty"`
	// 适用平台 位掩码（0=未声明，不参与平台门控）取自 frontmatter platforms
	//  * `linux`
	//  * `macos` - macOS
	//  * `windows`
	Platform *Platform `enums:"linux,macos,windows" extensions:"x-order=H" json:"platform" swaggertype:"string"`
	// 分类 取自 frontmatter metadata.hermes.category
	Category *string `extensions:"x-order=I" form:"category" json:"category,omitempty"`
	// 主页 取自 frontmatter metadata.hermes.homepage
	Homepage *string `extensions:"x-order=J" form:"homepage" json:"homepage,omitempty"`
	// 关联技能名 取自 frontmatter metadata.hermes.related_skills
	RelatedSkills *[]string `extensions:"x-order=K" json:"relatedSkills,omitempty"`
	// for meta update
	MetaDiff *comm.MetaDiff `json:"metaUp,omitempty" swaggerignore:"true"`
} // @name skillsSkillSet

func (z *Skill) SetWith(o SkillSet) {
	if o.Name != nil && z.Name != *o.Name {
		z.LogChangeValue("name", z.Name, o.Name)
		z.Name = *o.Name
	}
	if o.Description != nil && z.Description != *o.Description {
		z.LogChangeValue("description", z.Description, o.Description)
		z.Description = *o.Description
	}
	if o.Content != nil && z.Content != *o.Content {
		z.LogChangeValue("content", z.Content, o.Content)
		z.Content = *o.Content
	}
	if o.Channel != nil {
		z.LogChangeValue("channel", z.Channel, o.Channel)
		z.Channel = *o.Channel
	}
	if o.Version != nil && z.Version != *o.Version {
		z.LogChangeValue("version", z.Version, o.Version)
		z.Version = *o.Version
	}
	if o.Author != nil && z.Author != *o.Author {
		z.LogChangeValue("author", z.Author, o.Author)
		z.Author = *o.Author
	}
	if o.License != nil && z.License != *o.License {
		z.LogChangeValue("license", z.License, o.License)
		z.License = *o.License
	}
	if o.Platform != nil {
		z.LogChangeValue("platform", z.Platform, o.Platform)
		z.Platform = *o.Platform
	}
	if o.Category != nil && z.Category != *o.Category {
		z.LogChangeValue("category", z.Category, o.Category)
		z.Category = *o.Category
	}
	if o.Homepage != nil && z.Homepage != *o.Homepage {
		z.LogChangeValue("homepage", z.Homepage, o.Homepage)
		z.Homepage = *o.Homepage
	}
	if o.RelatedSkills != nil {
		z.LogChangeValue("related_skills", z.RelatedSkills, o.RelatedSkills)
		z.RelatedSkills = *o.RelatedSkills
	}
	if o.MetaDiff != nil && z.MetaUp(o.MetaDiff) {
		z.SetChange("meta")
	}
}
func (in *SkillBasic) MetaAddKVs(args ...any) *SkillBasic {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}
func (in *SkillSet) MetaAddKVs(args ...any) *SkillSet {
	in.MetaDiff = comm.MetaDiffAddKVs(in.MetaDiff, args...)
	return in
}

// consts of File 技能资源文件
const (
	FileTable = "agent_skill_file"
	FileAlias = "sf"
	FileLabel = "file"
	FileTypID = "skillsFile"
)

// File 技能资源文件
type File struct {
	comm.BaseModel `bun:"table:agent_skill_file,alias:sf" json:"-"`

	comm.DefaultModel

	FileBasic
} // @name skillsFile

type FileBasic struct {
	// 所属技能 id
	SkillID oid.OID `bson:"skillId" bun:"skill_id,notnull,type:bigint,unique:uk_skill_path" extensions:"x-order=A" json:"skillId" pg:"skill_id,notnull,type:bigint,unique:uk_skill_path" swaggertype:"string"`
	// 资源文件相对路径 技能内唯一
	Path string `bson:"path" bun:",notnull,type:varchar(255),unique:uk_skill_path" extensions:"x-order=B" form:"path" json:"path" pg:",notnull,type:varchar(255),unique:uk_skill_path"`
	// 文件内容 文本按 UTF-8 字节存
	Content []byte `bson:"content" bun:",notnull,type:bytea" extensions:"x-order=C" json:"content,omitempty" pg:",notnull,type:bytea"`
	// 内容类型
	Mime string `bson:"mime" bun:",notnull,type:varchar(128)" extensions:"x-order=D" form:"mime" json:"mime,omitempty" pg:",notnull,type:varchar(128)"`
	// 文件类型
	//  * `text` - 文本
	//  * `binary` - 二进制
	Kind FileKind `bson:"kind" bun:",notnull,type:smallint" enums:"text,binary" extensions:"x-order=E" json:"kind,omitempty" pg:",notnull,type:smallint" swaggertype:"string"`
	// 字节数
	Size int64 `bson:"size" bun:",notnull,type:bigint" extensions:"x-order=F" form:"size" json:"size" pg:",notnull,type:bigint"`
} // @name skillsFileBasic

type Files []File

// Creating function call to it's inner fields defined hooks
func (z *File) Creating() error {
	if z.IsZeroID() {
		z.SetID(oid.NewID(oid.OtFile))
	}

	return z.DefaultModel.Creating()
}
func NewFileWithBasic(in FileBasic) *File {
	obj := &File{
		FileBasic: in,
	}
	return obj
}
func NewFileWithID(id any) *File {
	obj := new(File)
	_ = obj.SetID(id)
	return obj
}
func (_ *File) IdentityLabel() string { return FileLabel }
func (_ *File) IdentityModel() string { return FileTypID }
func (_ *File) IdentityTable() string { return FileTable }
func (_ *File) IdentityAlias() string { return FileAlias }

type FileSet struct {
	// 文件内容 文本按 UTF-8 字节存
	Content *[]byte `extensions:"x-order=A" json:"content,omitempty"`
	// 内容类型
	Mime *string `extensions:"x-order=B" form:"mime" json:"mime,omitempty"`
	// 文件类型
	//  * `text` - 文本
	//  * `binary` - 二进制
	Kind *FileKind `enums:"text,binary" extensions:"x-order=C" json:"kind,omitempty" swaggertype:"string"`
	// 字节数
	Size *int64 `extensions:"x-order=D" json:"size"`
} // @name skillsFileSet

func (z *File) SetWith(o FileSet) {
	if o.Content != nil {
		z.LogChangeValue("content", z.Content, o.Content)
		z.Content = *o.Content
	}
	if o.Mime != nil && z.Mime != *o.Mime {
		z.LogChangeValue("mime", z.Mime, o.Mime)
		z.Mime = *o.Mime
	}
	if o.Kind != nil {
		z.LogChangeValue("kind", z.Kind, o.Kind)
		z.Kind = *o.Kind
	}
	if o.Size != nil && z.Size != *o.Size {
		z.LogChangeValue("size", z.Size, o.Size)
		z.Size = *o.Size
	}
}
