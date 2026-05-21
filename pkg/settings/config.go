package settings

import (
	"log"

	"github.com/kelseyhightower/envconfig"
)

// consts
const (
	Name = "Morign"
)

// Config ...
type Config struct {
	Name         string `ignored:"true"`
	Version      string `ignored:"true"`
	PgStoreDSN   string `envconfig:"PG_STORE_DSN" default:"postgres://morign@localhost/morign?sslmode=disable"`
	PgTSConfig   string `envconfig:"PG_TS_CONFIG"`
	PgQueryDebug bool   `envconfig:"PG_QUERY_DEBUG"`
	DbAutoInit   bool   `envconfig:"DB_AUTO_INIT"`
	SentryDSN    string `envconfig:"SENTRY_DSN" `
	HTTPListen   string `envconfig:"HTTP_LISTEN" default:":5001" required:"true"`
	APIPrefix    string `envconfig:"API_PREFIX" default:"/api" desc:"API path prefix"`
	RedisURI     string `envconfig:"redis_uri" default:"redis://localhost:6379/1" required:"true"`

	AllowOrigins []string `envconfig:"allow_origins" default:"*" desc:"cors"` // CORS: 允许的 Origin 调用来源
	AuthRequired bool     `envconfig:"Auth_Required"`
	AuthSecret   string   `envconfig:"Auth_Secret" desc:"for chatgpt-web session only"`
	CookieName   string   `envconfig:"Cookie_Name" default:"oaic" desc:"for oauth client"`
	CookiePath   string   `envconfig:"Cookie_Path" default:"/" desc:"for oauth client"`
	CookieDomain string   `envconfig:"Cookie_Domain" desc:"for oauth client"`
	CookieMaxAge int      `envconfig:"Cookie_MaxAge" desc:"seconds of cookie maxAge"`

	OAuthAuthMCP bool   `envconfig:"OAuth_Auth_MCP" desc:"OAuth MCP need authorized first"`
	OAuthName    string `envconfig:"OAuth_Name" default:"oauth" desc:"Name of OAuth SP"`
	OAuthPathMCP string `envconfig:"OAuth_Path_MCP" desc:"OAuth SP as A MCP Server"`

	// BusPrefix is the base URL for Bus API calls (used by capability invoke)
	BusPrefix string `envconfig:"Bus_Prefix" desc:"Prefix for Bus API"`
	BusResult string `envconfig:"Bus_Result" default:"result"`

	SitePathMe   string `envconfig:"Site_Path_Me" desc:"OAuth SP Path of /api/me in whole site"`
	SiteTokenKey string `envconfig:"Site_Token_Key" default:"token" desc:"token key in whole site"`

	StrataMCPURL string `envconfig:"Strata_MCP_URL"`

	WebAppPath string `envconfig:"Web_App_Path" default:"/" desc:"web app path for oauth redirect"`

	PresetFile  string `envconfig:"preset_file" desc:"custom welcome and messages"`
	QAEmbedding bool   `envconfig:"QA_Embedding" desc:"enable embed QA into prompt"`
	QAChatLog   bool   `envconfig:"QA_chat_log"`

	AskRateLimit string `envconfig:"Ask_Rate_Limit" default:"20-H"`

	DateInContext bool `envconfig:"date_in_context"`

	KeeperRole string   `envconfig:"Keeper_Role" default:"keeper" desc:"role required for write tools"`
	KeeperUIDs []string `envconfig:"Keeper_UIDs" desc:"uid list that bypasses role check"`

	// 相似度阈值 建议范围 0.39 - 0.65, 数值越大条件越宽
	VectorThreshold float32 `envconfig:"Vector_Threshold" default:"0.47"`
	// 相似度匹配数量
	VectorLimit int `envconfig:"Vector_Limit" default:"6"`

	// LLM调用循环次数限制，防止无限循环
	MaxLoopIterations int `envconfig:"MAX_LOOP_ITERATIONS" default:"12"`

	Embedding Provider
	Interact  Provider
	Summarize Provider
	Rerank    Provider

	// 是否启用 LLM 重排（默认关闭，验证效果后开启）
	RerankEnabled bool `envconfig:"RERANK_ENABLED" default:"false"`
	// 重排宽召回候选数量
	RerankRecallLimit int `envconfig:"RERANK_RECALL_LIMIT" default:"15"`
	// 重排结果缓存 TTL（秒）
	RerankCacheTTL int `envconfig:"RERANK_CACHE_TTL" default:"300"`
}

type Provider struct {
	APIKey string `envconfig:"Api_Key" `
	URL    string `envconfig:"url" `
	Model  string `envconfig:"MODEL" required:"true"`
	Type   string `envconfig:"type" default:"openai" desc:"provider type: openai, anthropic, openrouter, ollama"`
	Debug  bool   `envconfig:"debug" desc:"enable debug mode for this provider"`
	LogDir string `envconfig:"log_dir" desc:"directory to log LLM interactions, files named by date (jsonl format)"`

	Temperature    float64 `envconfig:"temperature"`
	TimeoutSeconds int     `envconfig:"timeout"`
}

func (c *Config) GetOAuthName() string {
	if len(c.OAuthName) > 0 {
		return c.OAuthName
	}
	return "oauth"
}

var (
	// Current 当前配置
	Current = new(Config)
)

func init() {
	if err := envconfig.Process(Name, Current); err != nil {
		log.Printf("envconfig process fail: %s", err)
	}

	Current.Name = Name
	Current.Version = version
}

// Usage 打印配置帮助
func Usage() error {
	log.Printf("ver: %s", Current.Version)
	return envconfig.Usage(Current.Name, Current)
}

// AllowAllOrigins ...
func AllowAllOrigins() bool {
	return 0 == len(Current.AllowOrigins) ||
		1 == len(Current.AllowOrigins) && Current.AllowOrigins[0] == "*"
}
