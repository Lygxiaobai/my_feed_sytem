package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	RabbitMQ  RabbitMQConfig  `yaml:"rabbitmq"`
	JWT       JWTConfig       `yaml:"jwt"`
	Upload    UploadConfig    `yaml:"upload"`
	Storage   StorageConfig   `yaml:"storage"`
	Pprof     PprofConfig     `yaml:"pprof"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	Log       LogConfig       `yaml:"log"`
	Audit     AuditConfig     `yaml:"audit"`
	Auth      AuthConfig      `yaml:"auth"`
	Ops       OpsConfig       `yaml:"ops"`
	Stripe    StripeConfig    `yaml:"stripe"`
	Feed      FeedConfig      `yaml:"feed"`
	Embedding EmbeddingConfig `yaml:"embedding"`
}

// FeedConfig 控制信息流的分发策略。
type FeedConfig struct {
	Fanout    FanoutConfig    `yaml:"fanout"`
	Recommend RecommendConfig `yaml:"recommend"`
}

// RecommendConfig 只服务首页推荐混排，与关注流 fanout 阈值脱钩。
type RecommendConfig struct {
	// SmallCreatorMaxFollowers 以下粉丝数的作者进入普通人队列。
	SmallCreatorMaxFollowers int64   `yaml:"small_creator_max_followers"`
	SlotSmallRatio           float64 `yaml:"slot_small_ratio"`
	MMRLambda                float64 `yaml:"mmr_lambda"`
}

const (
	defaultSmallCreatorMaxFollowers = int64(50)
	defaultSlotSmallRatio           = 0.2
	defaultMMRLambda                = 0.7
)

func (c *RecommendConfig) ApplyDefaults() {
	if c.SmallCreatorMaxFollowers <= 0 {
		c.SmallCreatorMaxFollowers = defaultSmallCreatorMaxFollowers
	}
	if c.SlotSmallRatio <= 0 || c.SlotSmallRatio > 0.8 {
		c.SlotSmallRatio = defaultSlotSmallRatio
	}
	if c.MMRLambda <= 0 || c.MMRLambda > 1 {
		c.MMRLambda = defaultMMRLambda
	}
}

// EmbeddingConfig 是外部 HTTP embedding 接口。url 或 key 为空时不调用，推荐退化为冷启动混排。
type EmbeddingConfig struct {
	APIURL         string `yaml:"api_url"`
	APIKey         string `yaml:"api_key"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

const defaultEmbeddingModel = "text-embedding-3-small"

func (c *EmbeddingConfig) ApplyDefaults() {
	if strings.TrimSpace(c.Model) == "" {
		c.Model = defaultEmbeddingModel
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 15
	}
}

func (c EmbeddingConfig) Enabled() bool {
	return strings.TrimSpace(c.APIURL) != "" && strings.TrimSpace(c.APIKey) != ""
}

// FanoutConfig 定义关注流「推拉结合」的分级阈值与容量。
//
// 按作者粉丝数分三档，目的是把写扩散的成本挡在大V 之外：
//
//	粉丝数 < PushThreshold                     全量写扩散，所有粉丝的收件箱都推
//	PushThreshold <= 粉丝数 < PullThreshold    只推活跃粉丝，冷粉丝上线时回源重建
//	粉丝数 >= PullThreshold                    完全不推，粉丝读取时从作者发件箱实时拉取
type FanoutConfig struct {
	PushThreshold int64 `yaml:"push_threshold"`
	PullThreshold int64 `yaml:"pull_threshold"`
	// BatchSize 是单条扩散消息处理的粉丝数量。消费端对单条消息有 10 秒处理上限，
	// 大V 一次推完必然超时，因此扩散按 follower_id 游标拆成多条消息。
	BatchSize int64 `yaml:"batch_size"`
	// InboxMaxSize 是每个用户收件箱保留的条数。收件箱未满即代表内容完整，
	// 读路径依赖这个不变量来判断能否信任「没有下一页」。
	InboxMaxSize  int64 `yaml:"inbox_max_size"`
	OutboxMaxSize int64 `yaml:"outbox_max_size"`
	// ActiveTTLHours 是活跃标记的存活时长，同时也是收件箱的维护窗口。
	ActiveTTLHours int `yaml:"active_ttl_hours"`
	// MaxPullAuthors 限制单次读取最多合并几个大V 发件箱。
	// 超过此数量说明该用户关注的大V 过多，读扩散成本已经超过直接查库，退回 MySQL。
	MaxPullAuthors int `yaml:"max_pull_authors"`
}

// 默认值按「中小规模部署也能直接跑」选取，生产环境应显式配置。
const (
	defaultFanoutPushThreshold  = int64(5000)
	defaultFanoutPullThreshold  = int64(100000)
	defaultFanoutBatchSize      = int64(1000)
	defaultFanoutInboxMaxSize   = int64(1000)
	defaultFanoutOutboxMaxSize  = int64(200)
	defaultFanoutActiveTTLHours = 24 * 7
	defaultFanoutMaxPullAuthors = 20
)

// ApplyDefaults 把未配置项补成默认值。
// 阈值为 0 时若按字面理解会退化成「所有人都是大V」，即关注流彻底不推，
// 与使用者的预期正好相反，所以这里必须兜底而不是放行。
// 装配方也应调用一次，避免绕过 Load 直接构造出零值配置。
func (c *FanoutConfig) ApplyDefaults() {
	if c.PushThreshold <= 0 {
		c.PushThreshold = defaultFanoutPushThreshold
	}
	if c.PullThreshold <= 0 {
		c.PullThreshold = defaultFanoutPullThreshold
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultFanoutBatchSize
	}
	if c.InboxMaxSize <= 0 {
		c.InboxMaxSize = defaultFanoutInboxMaxSize
	}
	if c.OutboxMaxSize <= 0 {
		c.OutboxMaxSize = defaultFanoutOutboxMaxSize
	}
	if c.ActiveTTLHours <= 0 {
		c.ActiveTTLHours = defaultFanoutActiveTTLHours
	}
	if c.MaxPullAuthors <= 0 {
		c.MaxPullAuthors = defaultFanoutMaxPullAuthors
	}
}

// ActiveTTL 返回活跃标记的存活时长。
func (c FanoutConfig) ActiveTTL() time.Duration {
	return time.Duration(c.ActiveTTLHours) * time.Hour
}

// StripeConfig 是测试用的 Checkout。密钥全部从环境变量展开，缺省时 Stripe 充值返回未配置。
type StripeConfig struct {
	SecretKey     string `yaml:"secret_key"`
	WebhookSecret string `yaml:"webhook_secret"`
	SuccessURL    string `yaml:"success_url"`
	Currency      string `yaml:"currency"`
}

// OpsConfig 是测试邮箱运维台用的只读观测地址。
type OpsConfig struct {
	LokiURL       string `yaml:"loki_url"`
	PrometheusURL string `yaml:"prometheus_url"`
}

// AuthConfig 控制登录方式。密钥类字段一律从环境变量展开。
type AuthConfig struct {
	Email EmailAuthConfig `yaml:"email"`
	SMTP  SMTPConfig      `yaml:"smtp"`
}

type EmailAuthConfig struct {
	TestDomain     string `yaml:"test_domain"`
	CodeTTLSeconds int    `yaml:"code_ttl_seconds"`
}

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	TLS      string `yaml:"tls"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
}

// AuditConfig 控制内容审核。
//
// 词库文件本身属于敏感资产（会被用来反推规则并试探绕过），
// 与密钥同等对待：不进版本库，通过路径引用。
type AuditConfig struct {
	// Enabled 是审核总开关。未配置或为 false 时发布即公开，
	// 不投递审核事件、不启动机审消费者、不挂人工复审接口。
	Enabled bool `yaml:"enabled"`
	// BlockWordFile 命中即拒绝的词库路径。
	BlockWordFile string `yaml:"block_word_file"`
	// ReviewWordFile 命中转人工的词库路径。
	ReviewWordFile string `yaml:"review_word_file"`
	// MediaPolicy 在未接入媒体审核能力时的处置方式：review（默认）或 pass。
	// 生产环境应保持 review——没有能力审核就不能假装审过了。
	MediaPolicy string `yaml:"media_policy"`
	// ReviewerAccountIDs 是有人工复审权限的账号。
	// 当前系统只有「是/不是审核员」一个区分，用白名单而不是引入一套 RBAC。
	ReviewerAccountIDs []uint64 `yaml:"reviewer_account_ids"`
}

// LogConfig 控制日志输出。
// Level 取 debug / info / warn / error，生产环境不应使用 debug；
// Format 取 json / text，线上用 json 以便被采集系统直接解析。
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type RabbitMQConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// VHost 为空时默认使用 "/"。
	VHost string `yaml:"vhost"`
	// PrefetchCount 控制每个消费者可同时持有的未 ACK 消息数量。
	PrefetchCount int `yaml:"prefetch_count"`
	// ConsumerTag 用于在 RabbitMQ 控制台区分消费者实例。
	ConsumerTag string `yaml:"consumer_tag"`
}

type JWTConfig struct {
	Secret string `yaml:"secret"`
}

type UploadConfig struct {
	Dir           string `yaml:"dir"`
	MaxVideoBytes int64  `yaml:"max_video_bytes"`
}

// StorageConfig 是对象存储（Silo，S3 兼容）的接入参数。
//
// Endpoint 与 PublicEndpoint 必须分开：前者是 compose 网络内地址，供 backend/worker
// 自己读写；后者是浏览器实际连接的地址，只用于给分片签预签名 URL。SigV4 把 Host
// 算进签名，用内网地址签出来的 URL 浏览器一用就是 403。
type StorageConfig struct {
	Endpoint       string `yaml:"endpoint"`
	PublicEndpoint string `yaml:"public_endpoint"`
	AccessKey      string `yaml:"access_key"`
	SecretKey      string `yaml:"secret_key"`
	UseSSL         bool   `yaml:"use_ssl"`
	Region         string `yaml:"region"`
	BucketUploads  string `yaml:"bucket_uploads"`
	BucketMedia    string `yaml:"bucket_media"`
}

type PprofConfig struct {
	API    PprofServerConfig `yaml:"api"`
	Worker PprofServerConfig `yaml:"worker"`
}

type PprofServerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

// MetricsConfig 只有 Worker 一项：API 进程的 /metrics 挂在业务路由上，
// 而 Worker 没有任何 HTTP 服务，指标需要单独开一个端口才能被抓取。
type MetricsConfig struct {
	Worker MetricsServerConfig `yaml:"worker"`
}

// MetricsServerConfig 描述附属指标端口。
// 该端口不对宿主机发布，只在 compose 网络内供 Prometheus 抓取。
type MetricsServerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

func Load(path string) (*Config, error) {
	// 本地开发从工作目录下的 .env 补齐环境变量（该文件已被 .gitignore 排除）。
	// 容器部署不带 .env，配置全部由编排层注入，两种场景走同一套代码。
	loadDotEnv(".env")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	expanded, missing := expandPlaceholders(string(data))
	if len(missing) > 0 {
		// 一次性列出全部缺失项，避免改一个报一个。
		return nil, fmt.Errorf(
			"配置缺少必需的环境变量: %s（本地开发可复制 backend/.env.example 为 backend/.env 并填写）",
			strings.Join(missing, ", "),
		)
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config file: %w", err)
	}

	cfg.Stripe.fillFromEnv()
	cfg.Feed.Fanout.ApplyDefaults()
	cfg.Feed.Recommend.ApplyDefaults()
	cfg.Embedding.ApplyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *StripeConfig) fillFromEnv() {
	if strings.TrimSpace(c.SecretKey) == "" {
		c.SecretKey = os.Getenv("STRIPE_SECRET_KEY")
	}
	if strings.TrimSpace(c.WebhookSecret) == "" {
		c.WebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	}
	if strings.TrimSpace(c.SuccessURL) == "" {
		c.SuccessURL = firstNonEmptyEnv("STRIPE_SUCCESS_URL", "https://lvmouren.indevs.in/wallet")
	}
	if strings.TrimSpace(c.Currency) == "" {
		c.Currency = firstNonEmptyEnv("STRIPE_CURRENCY", "usd")
	}
}

func firstNonEmptyEnv(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

// placeholderPattern 匹配 ${VAR} 与 ${VAR:-default} 两种写法。
var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandPlaceholders 展开配置中的环境变量占位符，并返回缺失的必填变量名。
//
// 不使用 os.Expand 的原因：它把未设置的变量静默展开为空字符串，
// 且不支持默认值语法。对密钥类配置而言这非常危险——缺少 JWT_SECRET
// 会得到一个空密钥，服务照常启动，但任何人都能伪造 token。
// 这里区分两种语义：
//
//	${VAR}            必填，未设置时收集进 missing 并让启动失败
//	${VAR:-default}   选填，未设置时使用默认值（只用于非敏感项）
func expandPlaceholders(raw string) (string, []string) {
	var missing []string
	seen := make(map[string]struct{})

	out := placeholderPattern.ReplaceAllStringFunc(raw, func(match string) string {
		groups := placeholderPattern.FindStringSubmatch(match)
		name := groups[1]
		hasDefault := strings.Contains(match, ":-")

		if value, ok := os.LookupEnv(name); ok && value != "" {
			return value
		}
		if hasDefault {
			return groups[2]
		}
		if _, dup := seen[name]; !dup {
			seen[name] = struct{}{}
			missing = append(missing, name)
		}
		return ""
	})

	return out, missing
}

// minJWTSecretLength 是 JWT 密钥的最小长度。
// HS256 的安全性完全取决于密钥强度，过短的密钥可被离线暴力破解。
const minJWTSecretLength = 32

// validate 在配置解析完成后做安全性兜底校验。
// 这类问题如果放到运行期才暴露，往往已经签发了一批不可信的 token。
func (c *Config) validate() error {
	secret := strings.TrimSpace(c.JWT.Secret)
	if secret == "" {
		return errors.New("jwt.secret 为空：请设置 JWT_SECRET 环境变量")
	}
	if len(secret) < minJWTSecretLength {
		return fmt.Errorf(
			"jwt.secret 长度不足 %d 位（当前 %d 位）：请用 openssl rand -base64 48 生成后设置 JWT_SECRET",
			minJWTSecretLength, len(secret),
		)
	}
	if c.Database.DBName == "" {
		return errors.New("database.dbname 为空：请检查配置文件")
	}
	// 两个阈值倒挂会让「只推活跃粉丝」这一档消失，且哪一档生效难以从行为上察觉，
	// 属于必须启动期拦下的配置错误。
	if c.Feed.Fanout.PullThreshold < c.Feed.Fanout.PushThreshold {
		return fmt.Errorf(
			"feed.fanout.pull_threshold(%d) 不能小于 push_threshold(%d)",
			c.Feed.Fanout.PullThreshold, c.Feed.Fanout.PushThreshold,
		)
	}
	return nil
}
