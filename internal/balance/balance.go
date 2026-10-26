package balance

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"whale-desktop/internal/config"
	"whale-desktop/internal/credentials"
	"whale-desktop/internal/ledger"
)

const balanceURL = "https://api.deepseek.com/user/balance"

// Price is [off-peak, peak] CNY per million tokens.
type Price struct {
	Hit  [2]float64
	Miss [2]float64
	Out  [2]float64
}

var basePrice = Price{Hit: [2]float64{0.05, 0.1}, Miss: [2]float64{1.5, 3}, Out: [2]float64{4.5, 9}}
var proPrice = Price{Hit: [2]float64{0.15, 0.3}, Miss: [2]float64{4.5, 9}, Out: [2]float64{13.5, 27}}

var pricing = map[string]Price{
	"deepseek-v4-flash-vision-exp": basePrice,
	"deepseek-v4-flash":            basePrice,
	"deepseek-v4-pro":              proPrice,
	"deepseek-chat":                basePrice,
	"deepseek-reasoner":            basePrice,
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)
var weekendValleyFrom = time.Date(2026, 8, 23, 0, 0, 0, 0, shanghai)

// PriceFor returns the current model price table.
func PriceFor(model string) Price {
	model = strings.ToLower(model)
	for name, price := range pricing {
		if strings.Contains(model, name) {
			return price
		}
	}
	return basePrice
}

// IsPeakTime applies the current weekday peak windows.
func IsPeakTime(unix int64) bool {
	at := time.Unix(unix, 0).In(shanghai)
	if !at.Before(weekendValleyFrom) {
		day := at.Weekday()
		if day == time.Saturday || day == time.Sunday {
			return false
		}
	}
	hour := at.Hour()
	return (hour >= 9 && hour < 12) || (hour >= 14 && hour < 18)
}

// Payload is shared by polling and renderer calls.
type Payload struct {
	OK             bool     `json:"ok"`
	Code           string   `json:"code,omitempty"`
	Error          string   `json:"error,omitempty"`
	Hint           string   `json:"hint,omitempty"`
	Transient      bool     `json:"transient,omitempty"`
	TotalBalance   *float64 `json:"totalBalance,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
	KeySource      string   `json:"keySource,omitempty"`
	IsPeak         bool     `json:"isPeak"`
	UsageMode      string   `json:"usageMode,omitempty"`
	TodayUsage     *float64 `json:"todayUsage,omitempty"`
	TodayTokens    *int64   `json:"todayTokens,omitempty"`
	UsageSource    string   `json:"usageSource,omitempty"`
	UsageWarning   string   `json:"usageWarning,omitempty"`
	Cached         bool     `json:"cached,omitempty"`
	Stale          bool     `json:"stale,omitempty"`
	Warning        string   `json:"warning,omitempty"`
	BalanceInfoRaw string   `json:"-"`
}

// Fetch requests the account balance with one retry for transient failures.
func Fetch(ctx context.Context, cfg config.Config) Payload {
	cred := credentials.APIClientKey(cfg.APIKey)
	if cred.Value == "" {
		return Payload{
			Code:  "NO_KEY",
			Error: "未配置 DEEPSEEK_API_KEY",
			Hint:  "在 config.jsonc 填 apiKey，或设置环境变量 DEEPSEEK_API_KEY，或先在 DSH 里配置该凭据",
		}
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, balanceURL, nil)
		if err != nil {
			return Payload{Code: "HTTP", Error: err.Error()}
		}
		req.Header.Set("Authorization", "Bearer "+cred.Value)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt == 0 {
				time.Sleep(500 * time.Millisecond)
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if resp.StatusCode < 500 {
				break
			}
			if attempt == 0 {
				time.Sleep(500 * time.Millisecond)
			}
			continue
		}
		var raw map[string]any
		decodeErr := json.NewDecoder(resp.Body).Decode(&raw)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return Payload{Code: "PARSE", Error: "余额接口返回不是合法 JSON"}
		}
		infos, _ := raw["balance_infos"].([]any)
		info := pickBalanceInfo(infos, cfg.Currency)
		if info == nil {
			return Payload{Code: "SHAPE", Error: "余额接口返回结构异常"}
		}
		amount, ok := number(info["total_balance"])
		if !ok {
			return Payload{Code: "SHAPE", Error: "余额接口返回结构异常"}
		}
		return Payload{
			OK:           true,
			TotalBalance: &amount,
			Currency:     firstNonEmpty(text(info["currency"]), "CNY"),
			UpdatedAt:    time.Now().Format(time.RFC3339),
			KeySource:    cred.Source,
			IsPeak:       IsPeakTime(time.Now().Unix()),
		}
	}
	message := "unknown error"
	if lastErr != nil {
		message = lastErr.Error()
	}
	transient := !strings.HasPrefix(message, "HTTP 4")
	return Payload{
		Code: "HTTP", Transient: transient,
		Error: "余额接口请求失败: " + truncate(message, 200),
	}
}

func pickBalanceInfo(infos []any, prefer string) map[string]any {
	want := strings.ToUpper(prefer)
	if want != "" {
		for _, raw := range infos {
			info, ok := raw.(map[string]any)
			if ok && strings.ToUpper(text(info["currency"])) == want {
				if _, valid := number(info["total_balance"]); valid {
					return info
				}
			}
		}
	}
	for _, raw := range infos {
		info, ok := raw.(map[string]any)
		if ok && text(info["currency"]) == "CNY" {
			if amount, valid := number(info["total_balance"]); valid && amount > 0 {
				return info
			}
		}
	}
	for _, raw := range infos {
		info, ok := raw.(map[string]any)
		if ok {
			if amount, valid := number(info["total_balance"]); valid && amount > 0 {
				return info
			}
		}
	}
	for _, raw := range infos {
		info, ok := raw.(map[string]any)
		if ok && text(info["currency"]) == "CNY" {
			return info
		}
	}
	if len(infos) > 0 {
		info, _ := infos[0].(map[string]any)
		return info
	}
	return nil
}

// Usage is a priced token total.
type Usage struct {
	Amount float64 `json:"amount"`
	Tokens int64   `json:"tokens"`
}

// ComputeUsage prices all usage buckets in a platform response.
func ComputeUsage(data map[string]any) *Usage {
	root := data
	if nested, ok := data["data"].(map[string]any); ok {
		root = nested
		if biz, ok := nested["biz_data"].(map[string]any); ok {
			root = biz
		}
	}
	series, _ := root["series"].([]any)
	var amount float64
	var tokens int64
	found := false
	for _, rawSeries := range series {
		item, ok := rawSeries.(map[string]any)
		if !ok {
			continue
		}
		price := PriceFor(text(item["model"]))
		buckets, _ := item["buckets"].([]any)
		for _, rawBucket := range buckets {
			bucket, ok := rawBucket.(map[string]any)
			if !ok {
				continue
			}
			usage, ok := bucket["usage"].(map[string]any)
			if !ok {
				continue
			}
			hit := intNumber(usage["PROMPT_CACHE_HIT_TOKEN"])
			miss := intNumber(usage["PROMPT_CACHE_MISS_TOKEN"])
			out := intNumber(usage["RESPONSE_TOKEN"])
			if hit+miss+out == 0 {
				continue
			}
			found = true
			tokens += hit + miss + out
			index := 0
			if ts := intNumber(bucket["time"]); IsPeakTime(ts) {
				index = 1
			}
			amount += float64(hit)/1e6*price.Hit[index] +
				float64(miss)/1e6*price.Miss[index] +
				float64(out)/1e6*price.Out[index]
		}
	}
	if !found {
		return nil
	}
	return &Usage{Amount: amount, Tokens: tokens}
}

// FetchPlatformUsage requests today's token buckets from the platform API.
func FetchPlatformUsage(ctx context.Context, cfg config.Config) *Usage {
	cred := credentials.PlatformToken(cfg.PlatformToken)
	if cred.Value == "" {
		return nil
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	_, offset := now.Zone()
	query := url.Values{}
	query.Set("start", strconv.FormatInt(start, 10))
	query.Set("end", strconv.FormatInt(start+86400, 10))
	query.Set("tz", strconv.Itoa(offset))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://platform.deepseek.com/api/v0/usage/by_api_key/amount?"+query.Encode(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+cred.Value)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	var data map[string]any
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		return nil
	}
	return ComputeUsage(data)
}

// NewLedgerPayload enriches a successful balance response with usage.
func NewLedgerPayload(paths config.Paths, ctx context.Context, cfg config.Config, balance Payload) Payload {
	if !balance.OK || balance.TotalBalance == nil {
		return balance
	}
	led := ledger.Record(paths, *balance.TotalBalance, balance.Currency)
	balance.IsPeak = IsPeakTime(time.Now().Unix())
	balance.UsageMode = "ledger"
	balance.TodayUsage = &led.TodayUsage
	balance.UsageSource = "小鲸鱼记账"
	if cfg.UsageMode == "token" && credentials.PlatformToken(cfg.PlatformToken).Value != "" {
		if usage := FetchPlatformUsage(ctx, cfg); usage != nil {
			balance.UsageMode = "token"
			balance.TodayUsage = &usage.Amount
			balance.TodayTokens = &usage.Tokens
			balance.UsageSource = "平台接口"
		} else {
			balance.UsageWarning = "平台令牌不可用，已回退到记账模式"
		}
	}
	return balance
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed)
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func intNumber(value any) int64 {
	n, ok := number(value)
	if !ok {
		return 0
	}
	return int64(n)
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
