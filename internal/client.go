package internal

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"time"
)

const gatewayAPI = "https://jhb-gateway.haiyuebiz.com"
const maxRateLimitRetries = 5

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{Timeout: 25 * time.Second, Jar: jar},
	}
}

func (c *Client) request(ctx context.Context, endpoint, method string, body io.Reader, token string) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json;charset=UTF-8")
		}
		if token != "" {
			req.Header.Set("agenttoken", token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests && method == http.MethodGet && attempt < maxRateLimitRetries {
			delay := rateLimitDelay(resp.Header.Get("Retry-After"), attempt)
			_ = resp.Body.Close()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		responseBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("上游 HTTP %d", resp.StatusCode)
		}
		if !json.Valid(responseBody) {
			return nil, io.ErrUnexpectedEOF
		}
		return responseBody, nil
	}
}

func rateLimitDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(retryAfter); err == nil {
		if delay := time.Until(date); delay > 0 {
			return delay
		}
		return 0
	}
	return time.Duration(2<<attempt) * time.Second
}

func (c *Client) PublicKey(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, gatewayAPI+"/pagent/agent-portal/auth/public-key", http.MethodGet, nil, "")
}

func (c *Client) Login(ctx context.Context, account, credential string) (json.RawMessage, error) {
	body, err := json.Marshal(struct {
		Account    string `json:"account"`
		Credential string `json:"credential"`
		SMS        bool   `json:"sms"`
	}{Account: account, Credential: credential, SMS: false})
	if err != nil {
		return nil, err
	}
	return c.request(ctx, gatewayAPI+"/pagent/agent-portal/auth/login", http.MethodPost, bytes.NewReader(body), "")
}

func encryptPassword(publicKey, password string) (string, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return "", err
	}
	parsed, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		return "", err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("无效的 RSA 公钥")
	}
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, key, []byte(password))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

type Agent struct {
	ID                   string `json:"id"`
	Nickname             string `json:"nickname"`
	RecommendCode        string `json:"recommendCode"`
	Mobile               string `json:"mobile"`
	RecommenderName      string `json:"recommenderName"`
	RecommenderCode      string `json:"recommenderCode"`
	AgentLevel           int    `json:"agentLevel"`
	CreateTime           string `json:"createTime"`
	AuthenticationStatus *int   `json:"authenticationStatus"`
	Status               int    `json:"status"`
}

type AgentPage struct {
	PageNum  int     `json:"pageNum"`
	PageSize int     `json:"pageSize"`
	Total    int     `json:"total"`
	Pages    int     `json:"pages"`
	List     []Agent `json:"list"`
}

type MerchantPage struct {
	Pages int        `json:"pages"`
	List  []Merchant `json:"list"`
}

type Merchant map[string]json.RawMessage

type Terminal map[string]json.RawMessage

type TradePage struct {
	Pages int               `json:"pages"`
	List  []json.RawMessage `json:"list"`
}

type AgentWithData struct {
	Agent
	Terminals []Terminal        `json:"auto_terminals"`
	Trades    []json.RawMessage `json:"auto_trades"`
}

type AgentWithDescendants struct {
	AgentWithData
	Descendants []AgentWithData `json:"auto_descendants"`
}

type AgentResult struct {
	Total int                    `json:"total"`
	List  []AgentWithDescendants `json:"list"`
}

func (c *Client) AgentList(ctx context.Context, token, recommendCode string, pageNum int) (json.RawMessage, error) {
	query := url.Values{
		"recommendCode": {recommendCode},
		"pageNum":       {strconv.Itoa(pageNum)},
		"pageSize":      {"100"},
	}
	endpoint := gatewayAPI + "/pagent/agent-portal/query/agent?" + query.Encode()
	return c.request(ctx, endpoint, http.MethodGet, nil, token)
}

func (c *Client) AgentDescendants(ctx context.Context, token, agentID string, pageNum int) (json.RawMessage, error) {
	query := url.Values{
		"pageNum":  {strconv.Itoa(pageNum)},
		"pageSize": {"100"},
	}
	endpoint := gatewayAPI + "/pagent/agent-portal/query/agent/" + url.PathEscape(agentID) + "/descendants?" + query.Encode()
	return c.request(ctx, endpoint, http.MethodGet, nil, token)
}

func (c *Client) MerchantListBySN(ctx context.Context, token, sn string, pageNum int) (json.RawMessage, error) {
	query := url.Values{
		"sn":       {sn},
		"pageNum":  {strconv.Itoa(pageNum)},
		"pageSize": {"100"},
	}
	endpoint := gatewayAPI + "/pagent/agent-portal/query/merchant?" + query.Encode()
	return c.request(ctx, endpoint, http.MethodGet, nil, token)
}

func (c *Client) MerchantDetail(ctx context.Context, token, merchantID string) (json.RawMessage, error) {
	endpoint := gatewayAPI + "/pagent/agent-portal/query/merchant/" + url.PathEscape(merchantID) + "/detail"
	return c.request(ctx, endpoint, http.MethodGet, nil, token)
}

func (c *Client) TerminalList(ctx context.Context, token, recommendCode, startTime, endTime string, pageNum int) (json.RawMessage, error) {
	query := url.Values{
		"recommendCode": {recommendCode},
		"startTime":     {startTime},
		"endTime":       {endTime},
		"pageNum":       {strconv.Itoa(pageNum)},
		"pageSize":      {"100"},
	}
	endpoint := gatewayAPI + "/pagent/agent-portal/query/terminal?" + query.Encode()
	return c.request(ctx, endpoint, http.MethodGet, nil, token)
}

func (c *Client) TradeList(ctx context.Context, token, recommendCode, startTime, endTime string, pageNum int) (json.RawMessage, error) {
	query := url.Values{
		"recommendCode": {recommendCode},
		"startTime":     {startTime},
		"endTime":       {endTime},
		"pageNum":       {strconv.Itoa(pageNum)},
		"pageSize":      {"100"},
	}
	endpoint := gatewayAPI + "/pagent/agent-portal/query/trade?" + query.Encode()
	return c.request(ctx, endpoint, http.MethodGet, nil, token)
}
