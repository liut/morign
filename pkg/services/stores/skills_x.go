package stores

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cupogo/andvari/models/oid"
	"github.com/cupogo/andvari/stores/pgx"

	"github.com/liut/morign/pkg/models/mcps"
	"github.com/liut/morign/pkg/models/skills"
)

// SkillStoreX 技能存储扩展接口
type SkillStoreX interface {
	ListVisibleMetadata(ctx context.Context, spec *SkillSpec) (data skills.Skills, total int, err error)
	TopRecent(ctx context.Context, limit int) (data skills.Skills, err error)
	LoadForName(ctx context.Context, name string) (*skills.Skill, error)
	CreateSkillWithFiles(ctx context.Context, in skills.SkillBasic, files map[string]string) (obj *skills.Skill, err error)
	UpdateSkillWithFiles(ctx context.Context, id string, set skills.SkillSet, files map[string]string) error
	ImportSkillMD(ctx context.Context, content string, opt SkillImportOptions) (*skills.Skill, error)
	ListFileNames(ctx context.Context, name string) (skills.Files, error)
	ReadFile(ctx context.Context, name, path string) (*skills.File, error)
}

var ErrSkillNotFound = errors.New("skill not found")
var ErrFileNotFound = errors.New("file not found")

// ErrSkillNeedOwner 导入时既无上下文用户也未指定归属（CLI 必须显式给出归属）。
var ErrSkillNeedOwner = errors.New("skill owner required")

// SkillImportOptions 导入的可选覆盖项；零值表示用上下文用户与未投放（私有）。
type SkillImportOptions struct {
	Owner   oid.OID
	Channel skills.Channel
}

const (
	maxSkillFileSize   = 1 << 20  // 单文件大小上限 1MB
	maxSkillBundleSize = 10 << 20 // 单技能资源总量上限 10MB
)

// channelBit 返回当前上下文的频道位掩码；空频道名按 web 处理（HTTP 请求无频道上下文）。
func channelBit(ctx context.Context) skills.Channel {
	switch mcps.ChannelFromContext(ctx) {
	case "wecom":
		return skills.ChannelWecom
	case "feishu":
		return skills.ChannelFeishu
	default:
		return skills.ChannelWeb
	}
}

// skillVisibleCond 可见性条件：Channel 显式投放的频道 或 当前用户创建。
// 未投放（0）仅创建者可见。
func skillVisibleCond(ctx context.Context) (string, []any) {
	conds := []string{"channel & ? != 0"}
	args := []any{channelBit(ctx)}
	if user, ok := UserFromContext(ctx); ok {
		conds = append(conds, "owner = ?")
		args = append(args, oid.Cast(user.OID))
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// skillVisible 与 skillVisibleCond 同规则：显式投放的频道 或 当前用户创建。
func skillVisible(ctx context.Context, obj *skills.Skill) bool {
	if user, ok := UserFromContext(ctx); ok && obj.Owner == oid.Cast(user.OID) {
		return true
	}
	return obj.Channel&channelBit(ctx) != 0
}

// SiftX 按 spec.VisibleOnly 注入可见性过滤；始终裁剪元数据并默认时间排序。
func (spec *SkillSpec) SiftX(ctx context.Context, q *ormQuery) *ormQuery {
	if spec.VisibleOnly {
		cond, args := skillVisibleCond(ctx)
		q = q.Where(cond, args...)
	}
	q = q.ExcludeColumn("content")
	if spec.Sort == "" {
		spec.Sort = "created DESC"
	}
	return q
}

// ListVisibleMetadata 返回可见范围内的技能元数据（不含 SKILL.md 全文）。
// VisibleOnly 由本方法强制开启，避免客户端以 visible=false 绕过可见性；管理端 ListSkill 不受限。
func (s *skillStore) ListVisibleMetadata(ctx context.Context, spec *SkillSpec) (data skills.Skills, total int, err error) {
	spec.VisibleOnly = true
	total, err = s.w.db.ListModel(ctx, spec, &data)
	return
}

// TopRecent 返回可见技能中按创建时间最新的前 limit 条（元数据）。
func (s *skillStore) TopRecent(ctx context.Context, limit int) (data skills.Skills, err error) {
	spec := &SkillSpec{}
	if limit > 0 {
		spec.Limit = limit
	}
	data, _, err = s.ListVisibleMetadata(ctx, spec)
	return
}

// LoadForName 加载指定技能（含全文）。name 全局唯一，先按名加载（dbGetWith）、
// 再在 Go 层校验可见性；越权或不存在统一返回 ErrSkillNotFound（不泄露存在性）。
// 工具、指令注入与详情 API 共用此加载器。
func (s *skillStore) LoadForName(ctx context.Context, name string) (*skills.Skill, error) {
	obj := new(skills.Skill)
	err := dbGetWith(ctx, s.w.db, obj, "name", "=", name)
	if err != nil {
		if errors.Is(err, ErrNoRows) || errors.Is(err, ErrNotFound) {
			return nil, errors.Join(ErrSkillNotFound, err)
		}
		return nil, err
	}
	if !skillVisible(ctx, obj) {
		return nil, ErrSkillNotFound
	}
	return obj, nil
}
func dbBeforeCreateSkill(ctx context.Context, db ormDB, obj *skills.Skill) error {
	if obj.Owner.IsZero() {
		user, ok := UserFromContext(ctx)
		if ok {
			obj.Owner = oid.Cast(user.OID)
		}
	}

	return nil
}

// SiftX 文件清单查询始终裁剪内容列，避免加载 bytea。
func (spec *FileSpec) SiftX(ctx context.Context, q *ormQuery) *ormQuery {
	return q.ExcludeColumn("content")
}

// ListFileNames 返回指定可见技能的资源文件清单（不含内容）。
func (s *skillStore) ListFileNames(ctx context.Context, name string) (skills.Files, error) {
	obj, err := s.LoadForName(ctx, name)
	if err != nil {
		return nil, err
	}
	data, _, err := s.ListFile(ctx, &FileSpec{SkillID: obj.ID.String()})
	return data, err
}

// ReadFile 返回指定可见技能的单资源文件（含内容）。
func (s *skillStore) ReadFile(ctx context.Context, name, path string) (*skills.File, error) {
	obj, err := s.LoadForName(ctx, name)
	if err != nil {
		return nil, err
	}
	file := new(skills.File)
	err = s.w.db.NewSelect().Model(file).
		Where("skill_id = ?", obj.ID).
		Where("path = ?", path).
		Limit(1).Scan(ctx)
	if err != nil {
		if errors.Is(err, ErrNoRows) || errors.Is(err, ErrNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return file, nil
}

// ImportSkillMD 按 SKILL.md 正文导入技能：name/description 与元数据以 frontmatter 为权威，
// 调用方无需（也无法）另外指定名称与描述。同名冲突返回 ErrDuplicate，不覆盖既有行。
// 资源文件不在本期范围（.zip/.skill 与目录导入随下一期一并设计）。
func (s *skillStore) ImportSkillMD(ctx context.Context, content string, opt SkillImportOptions) (*skills.Skill, error) {
	// Frontmatter 仅切一次：拿 meta 校验、把 body 落库为 Skill.Content。
	// Skill.Content 不再含 YAML frontmatter。
	meta, body, ok := skills.Frontmatter(content)
	if !ok {
		return nil, skills.ErrFrontmatterMiss
	}
	if !skills.ValidName(meta.Name) {
		return nil, skills.ErrInvalidName
	}
	if !skills.ValidDescription(meta.Description) {
		return nil, skills.ErrDescriptionLong
	}
	if err := meta.Validate(); err != nil {
		return nil, err
	}
	owner := opt.Owner
	if owner.IsZero() {
		user, ok := UserFromContext(ctx)
		if !ok {
			return nil, ErrSkillNeedOwner
		}
		owner = oid.Cast(user.OID)
	}
	// 创建路径是 ON CONFLICT (name) DO UPDATE（仅更新 updated），不会报冲突；
	// 导入要求同名不覆盖，所以这里显式判存在。并发导入同名时后者会落在同一行上，
	// 结果仍是既有行而非覆盖，符合「不覆盖」的语义。
	exists, err := s.w.db.NewSelect().Model((*skills.Skill)(nil)).Where("name = ?", meta.Name).Exists(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDuplicate
	}
	in := skills.SkillBasic{
		Name:        meta.Name,
		Description: meta.Description,
		Content:     body,
		Channel:     opt.Channel,
		Owner:       owner,
	}
	// 七列元数据由 frontmatter 镜像到 in，避免依赖 CreateSkillWithFiles 内
	// 再次解析 in.Content（此时已是 body，无 frontmatter）。
	meta.ApplyTo(&in)
	return s.CreateSkillWithFiles(ctx, in, nil)
}

// CreateSkillWithFiles 创建技能并在同一事务内写入资源文件（与生成 CreateSkill 同逻辑，扩为 bundle 原子写）。
func (s *skillStore) CreateSkillWithFiles(ctx context.Context, in skills.SkillBasic, files map[string]string) (obj *skills.Skill, err error) {
	if err = checkSkillFiles(files); err != nil {
		return nil, err
	}
	err = s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		// 七列元数据由调用方在入参 SkillBasic 里填好；store 不再二次解析 Content。
		if obj, err = CreateSkill(ctx, tx, in); err != nil {
			return
		}
		return upsertSkillFiles(ctx, tx, obj.ID, files)
	})
	return
}

// UpdateSkillWithFiles 更新技能并在同一事务内按 bundle 替换资源文件；files 为 nil 时保留现有文件。
func (s *skillStore) UpdateSkillWithFiles(ctx context.Context, id string, set skills.SkillSet, files map[string]string) error {
	if files != nil {
		if err := checkSkillFiles(files); err != nil {
			return err
		}
	}
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		exist := new(skills.Skill)
		if err = dbGetWithPKID(ctx, tx, exist, id); err != nil {
			return
		}
		// 七列元数据由调用方在入参 SkillSet 里填好；store 不再二次解析 Content。
		exist.SetIsUpdate(true)
		exist.SetWith(set)
		dbMetaUp(ctx, tx, exist)
		if err = dbUpdate(ctx, tx, exist); err != nil {
			return
		}
		if files != nil {
			return upsertSkillFiles(ctx, tx, exist.ID, files)
		}
		return nil
	})
}

// dbBeforeDeleteSkill 技能删除前清理资源文件（由生成 DeleteSkill 在事务内调用）。
func dbBeforeDeleteSkill(ctx context.Context, db ormDB, obj *skills.Skill) error {
	_, err := db.NewDelete().Model((*skills.File)(nil)).Where("skill_id = ?", obj.ID).Exec(ctx)
	return err
}

// upsertSkillFiles 按 (skill_id, path) 复合键 upsert，并删除已不在 bundle 中的文件。
func upsertSkillFiles(ctx context.Context, db ormDB, skillID oid.OID, files map[string]string) error {
	paths := make([]string, 0, len(files))
	for path, content := range files {
		paths = append(paths, path)
		obj := skills.NewFileWithBasic(skills.FileBasic{
			SkillID: skillID,
			Path:    path,
			Content: []byte(content),
			Mime:    fileMimeFor(path),
			Kind:    detectFileKind(path, []byte(content)),
			Size:    int64(len(content)),
		})
		if err := obj.Creating(); err != nil {
			return err
		}
		if _, err := db.NewInsert().Model(obj).On(
			"CONFLICT (skill_id, path) DO UPDATE SET content = EXCLUDED.content, mime = EXCLUDED.mime, kind = EXCLUDED.kind, size = EXCLUDED.size, updated = EXCLUDED.updated",
		).Exec(ctx); err != nil {
			return err
		}
	}
	q := db.NewDelete().Model((*skills.File)(nil)).Where("skill_id = ?", skillID)
	if len(paths) > 0 {
		q.Where("path NOT IN (?)", pgx.In(paths))
	}
	_, err := q.Exec(ctx)
	return err
}

func checkSkillFiles(files map[string]string) error {
	var total int64
	for path, content := range files {
		if path == "" {
			return errors.New("empty file path")
		}
		size := int64(len(content))
		if size > maxSkillFileSize {
			return fmt.Errorf("file %s exceeds max size %d bytes", path, maxSkillFileSize)
		}
		total += size
	}
	if total > maxSkillBundleSize {
		return fmt.Errorf("skill bundle exceeds max size %d bytes", maxSkillBundleSize)
	}
	return nil
}

var binaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true, ".svg": true,
	".pdf": true, ".zip": true, ".gz": true, ".tar": true, ".7z": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".mp3": true, ".mp4": true, ".wav": true, ".mov": true,
}

func detectFileKind(path string, content []byte) skills.FileKind {
	if binaryExts[strings.ToLower(pathExt(path))] {
		return skills.FileKindBinary
	}
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		return skills.FileKindBinary
	}
	return skills.FileKindText
}

var mimeByExt = map[string]string{
	".md": "text/markdown", ".txt": "text/plain", ".py": "text/x-python",
	".js": "application/javascript", ".ts": "application/typescript", ".json": "application/json",
	".yaml": "application/yaml", ".yml": "application/yaml", ".sh": "text/x-shellscript",
	".go": "text/x-go", ".html": "text/html", ".css": "text/css", ".sql": "text/plain",
	".toml": "application/toml", ".xml": "application/xml", ".svg": "image/svg+xml",
}

func fileMimeFor(path string) string {
	if m, ok := mimeByExt[strings.ToLower(pathExt(path))]; ok {
		return m
	}
	return "application/octet-stream"
}

func pathExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}
