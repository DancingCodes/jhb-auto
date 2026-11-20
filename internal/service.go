package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	Account Account `json:"account"`
}

type Clients struct {
	Clients map[string]string `json:"clients"`
}

type Account struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	RecommendCode string `json:"recommendCode"`
}

func LoadClientAccounts(client string) (Config, error) {
	raw, err := os.ReadFile("config/clients.json")
	if err != nil {
		return Config{}, err
	}
	var clients Clients
	if err := json.Unmarshal(raw, &clients); err != nil {
		return Config{}, err
	}
	accountsFile := clients.Clients[client]
	if accountsFile == "" {
		return Config{}, os.ErrNotExist
	}
	raw, err = os.ReadFile(filepath.Join("config", accountsFile))
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	if config.Account.Username == "" || config.Account.Password == "" || config.Account.RecommendCode == "" {
		return Config{}, io.ErrUnexpectedEOF
	}
	return config, nil
}

func Login(ctx context.Context, account Account) (*Client, string, error) {
	client := NewClient()
	log.Println("获取登录公钥")
	publicKeyResponse, err := client.PublicKey(ctx)
	if err != nil {
		log.Printf("获取登录公钥失败: %v", err)
		return nil, "", errors.New("登录失败")
	}
	var publicKey struct {
		Data struct {
			PublicKey string `json:"publicKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(publicKeyResponse, &publicKey); err != nil || publicKey.Data.PublicKey == "" {
		log.Printf("解析登录公钥失败: %v", err)
		return nil, "", errors.New("登录失败")
	}
	log.Println("加密登录密码")
	credential, err := encryptPassword(publicKey.Data.PublicKey, account.Password)
	if err != nil {
		log.Printf("加密登录密码失败: %v", err)
		return nil, "", errors.New("登录失败")
	}
	log.Println("提交登录请求")
	loginResponse, err := client.Login(ctx, account.Username, credential)
	if err != nil {
		log.Printf("提交登录请求失败: %v", err)
		return nil, "", errors.New("登录失败")
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginResponse, &login); err != nil || login.Data.Token == "" {
		log.Printf("解析登录响应失败: %v", err)
		return nil, "", errors.New("登录失败")
	}
	log.Println("登录完成")
	return client, login.Data.Token, nil
}

func queryAgentHierarchy(ctx context.Context, client *Client, token, recommendCode string) (AgentResult, error) {
	var page AgentPage
	for pageNum := 1; ; pageNum++ {
		log.Printf("查询代理列表: 第 %d 页", pageNum)
		result, err := client.AgentList(ctx, token, recommendCode, pageNum)
		if err != nil {
			log.Printf("查询代理列表第 %d 页失败: %v", pageNum, err)
			return AgentResult{}, errors.New("代理列表获取失败")
		}
		var response struct {
			Data *AgentPage `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			log.Printf("解析代理列表第 %d 页失败: %v", pageNum, err)
			return AgentResult{}, errors.New("代理列表数据格式错误")
		}
		log.Printf("代理列表第 %d 页完成: %d 条", pageNum, len(response.Data.List))
		if pageNum == 1 {
			page = *response.Data
			if page.List == nil {
				page.List = []Agent{}
			}
		} else {
			page.List = append(page.List, response.Data.List...)
		}
		if pageNum >= page.Pages {
			break
		}
	}
	agents := AgentResult{Total: page.Total, List: make([]AgentWithDescendants, 0, len(page.List))}
	for _, agent := range page.List {
		log.Printf("处理代理: id=%s", agent.ID)
		item := AgentWithDescendants{
			AgentWithData: AgentWithData{Agent: agent, Merchants: []Merchant{}},
			Descendants:   []AgentWithData{},
		}
		for pageNum := 1; ; pageNum++ {
			log.Printf("查询代理下级: id=%s, 第 %d 页", agent.ID, pageNum)
			result, err := client.AgentDescendants(ctx, token, agent.ID, pageNum)
			if err != nil {
				log.Printf("查询代理下级失败: id=%s, 第 %d 页: %v", agent.ID, pageNum, err)
				return AgentResult{}, errors.New("代理下级获取失败")
			}
			var response struct {
				Data *AgentPage `json:"data"`
			}
			if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
				log.Printf("解析代理下级失败: id=%s, 第 %d 页: %v", agent.ID, pageNum, err)
				return AgentResult{}, errors.New("代理下级数据格式错误")
			}
			log.Printf("代理下级完成: id=%s, 第 %d 页, %d 条", agent.ID, pageNum, len(response.Data.List))
			for _, descendant := range response.Data.List {
				item.Descendants = append(item.Descendants, AgentWithData{Agent: descendant, Merchants: []Merchant{}})
			}
			if pageNum >= response.Data.Pages {
				break
			}
		}
		agents.List = append(agents.List, item)
		log.Printf("代理处理完成: id=%s", agent.ID)
	}
	log.Printf("代理及下级查询完成: %d 条代理", len(agents.List))
	return agents, nil
}

func QueryAgents(ctx context.Context, date string, config Config) (AgentResult, error) {
	client, token, err := Login(ctx, config.Account)
	if err != nil {
		return AgentResult{}, err
	}
	agents, err := queryAgentHierarchy(ctx, client, token, config.Account.RecommendCode)
	if err != nil {
		return AgentResult{}, err
	}
	for i := range agents.List {
		agent := &agents.List[i]
		log.Printf("查询代理商户: id=%s", agent.ID)
		merchants, err := queryMerchants(ctx, client, token, agent.RecommendCode, date)
		if err != nil {
			return AgentResult{}, err
		}
		agent.Merchants = merchants
		log.Printf("查询代理交易: id=%s", agent.ID)
		trades, err := queryTrades(ctx, client, token, agent.RecommendCode, date)
		if err != nil {
			return AgentResult{}, err
		}
		agent.Trades = trades
		for j := range agent.Descendants {
			descendant := &agent.Descendants[j]
			log.Printf("查询代理下级商户: id=%s", descendant.ID)
			merchants, err := queryMerchants(ctx, client, token, descendant.RecommendCode, date)
			if err != nil {
				return AgentResult{}, err
			}
			descendant.Merchants = merchants
			log.Printf("查询代理下级交易: id=%s", descendant.ID)
			trades, err := queryTrades(ctx, client, token, descendant.RecommendCode, date)
			if err != nil {
				return AgentResult{}, err
			}
			descendant.Trades = trades
		}
	}
	log.Printf("代理、商户和交易查询完成: %d 条代理", len(agents.List))
	return agents, nil
}

type MergedAgentResult struct {
	Merchants []Merchant        `json:"auto_merchants"`
	Trades    []json.RawMessage `json:"auto_trades"`
}

func QueryMergedAgents(ctx context.Context, date string, config Config) (MergedAgentResult, error) {
	agents, err := QueryAgents(ctx, date, config)
	if err != nil {
		return MergedAgentResult{}, err
	}
	result := MergedAgentResult{Merchants: []Merchant{}, Trades: []json.RawMessage{}}
	for _, agent := range agents.List {
		result.Merchants = append(result.Merchants, agent.Merchants...)
		result.Trades = append(result.Trades, agent.Trades...)
		for _, descendant := range agent.Descendants {
			result.Merchants = append(result.Merchants, descendant.Merchants...)
			result.Trades = append(result.Trades, descendant.Trades...)
		}
	}
	return result, nil
}

type DescendantCount struct {
	ID            string `json:"id"`
	MerchantCount int    `json:"auto_merchant_count"`
}

type AgentCount struct {
	ID              string            `json:"id"`
	DescendantCount int               `json:"auto_descendant_count"`
	MerchantCount   int               `json:"auto_merchant_count"`
	Descendants     []DescendantCount `json:"auto_descendants"`
}

type AgentCountResult struct {
	AgentCount      int          `json:"auto_agent_count"`
	DescendantCount int          `json:"auto_descendant_count"`
	MerchantCount   int          `json:"auto_merchant_count"`
	Agents          []AgentCount `json:"auto_agents"`
}

func QueryAgentCounts(ctx context.Context, date string, config Config) (AgentCountResult, error) {
	client, token, err := Login(ctx, config.Account)
	if err != nil {
		return AgentCountResult{}, err
	}
	agents, err := queryAgentHierarchy(ctx, client, token, config.Account.RecommendCode)
	if err != nil {
		return AgentCountResult{}, err
	}
	counts := AgentCountResult{AgentCount: len(agents.List), Agents: make([]AgentCount, 0, len(agents.List))}
	for _, agent := range agents.List {
		merchantCount, err := queryMerchantCount(ctx, client, token, agent.RecommendCode, date)
		if err != nil {
			return AgentCountResult{}, err
		}
		item := AgentCount{
			ID:              agent.ID,
			DescendantCount: len(agent.Descendants),
			MerchantCount:   merchantCount,
			Descendants:     make([]DescendantCount, 0, len(agent.Descendants)),
		}
		counts.DescendantCount += item.DescendantCount
		counts.MerchantCount += merchantCount
		for _, descendant := range agent.Descendants {
			merchantCount, err := queryMerchantCount(ctx, client, token, descendant.RecommendCode, date)
			if err != nil {
				return AgentCountResult{}, err
			}
			item.Descendants = append(item.Descendants, DescendantCount{ID: descendant.ID, MerchantCount: merchantCount})
			counts.MerchantCount += merchantCount
		}
		counts.Agents = append(counts.Agents, item)
	}
	return counts, nil
}

func queryMerchantCount(ctx context.Context, client *Client, token, recommendCode, date string) (int, error) {
	log.Printf("查询商户数量: recommendCode=%s", recommendCode)
	result, err := client.MerchantList(ctx, token, recommendCode, date+" 00:00:00", date+" 23:59:59", 1)
	if err != nil {
		log.Printf("查询商户数量失败: recommendCode=%s: %v", recommendCode, err)
		return 0, errors.New("商户数量获取失败")
	}
	var response struct {
		Data *MerchantPage `json:"data"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Data == nil || response.Data.Total == nil {
		log.Printf("解析商户数量失败: recommendCode=%s: %v", recommendCode, err)
		return 0, errors.New("商户数量数据格式错误")
	}
	log.Printf("商户数量完成: recommendCode=%s, %d 条", recommendCode, *response.Data.Total)
	return *response.Data.Total, nil
}

func queryMerchants(ctx context.Context, client *Client, token, recommendCode, date string) ([]Merchant, error) {
	merchants := []Merchant{}
	startTime := date + " 00:00:00"
	endTime := date + " 23:59:59"
	for pageNum := 1; ; pageNum++ {
		log.Printf("查询商户列表: recommendCode=%s, 第 %d 页", recommendCode, pageNum)
		result, err := client.MerchantList(ctx, token, recommendCode, startTime, endTime, pageNum)
		if err != nil {
			log.Printf("查询商户列表失败: recommendCode=%s, 第 %d 页: %v", recommendCode, pageNum, err)
			return nil, errors.New("商户列表获取失败")
		}
		var response struct {
			Data *MerchantPage `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			log.Printf("解析商户列表失败: recommendCode=%s, 第 %d 页: %v", recommendCode, pageNum, err)
			return nil, errors.New("商户列表数据格式错误")
		}
		log.Printf("商户列表完成: recommendCode=%s, 第 %d 页, %d 条", recommendCode, pageNum, len(response.Data.List))
		for _, merchant := range response.Data.List {
			var merchantID string
			if err := json.Unmarshal(merchant["id"], &merchantID); err != nil || merchantID == "" {
				log.Printf("解析商户 ID 失败: recommendCode=%s, 第 %d 页: %v", recommendCode, pageNum, err)
				return nil, errors.New("商户列表数据格式错误")
			}
			log.Printf("查询商户详情: id=%s", merchantID)
			result, err := client.MerchantDetail(ctx, token, merchantID)
			if err != nil {
				log.Printf("查询商户详情失败: id=%s: %v", merchantID, err)
				return nil, fmt.Errorf("商户 %s 详情获取失败: %w", merchantID, err)
			}
			var detail struct {
				Data *json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(result, &detail); err != nil || detail.Data == nil {
				log.Printf("解析商户详情失败: id=%s: %v", merchantID, err)
				return nil, errors.New("商户详情数据格式错误")
			}
			merchant["auto_detail"] = *detail.Data
			merchants = append(merchants, merchant)
			log.Printf("商户详情完成: id=%s", merchantID)
		}
		if pageNum >= response.Data.Pages {
			break
		}
	}
	log.Printf("商户查询完成: recommendCode=%s, %d 条", recommendCode, len(merchants))
	return merchants, nil
}

func queryTrades(ctx context.Context, client *Client, token, recommendCode, date string) ([]json.RawMessage, error) {
	trades := []json.RawMessage{}
	startTime := date + " 00:00:00"
	endTime := date + " 23:59:59"
	for pageNum := 1; ; pageNum++ {
		log.Printf("查询交易列表: recommendCode=%s, 第 %d 页", recommendCode, pageNum)
		result, err := client.TradeList(ctx, token, recommendCode, startTime, endTime, pageNum)
		if err != nil {
			log.Printf("查询交易列表失败: recommendCode=%s, 第 %d 页: %v", recommendCode, pageNum, err)
			return nil, errors.New("交易列表获取失败")
		}
		var response struct {
			Data *TradePage `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			log.Printf("解析交易列表失败: recommendCode=%s, 第 %d 页: %v", recommendCode, pageNum, err)
			return nil, errors.New("交易列表数据格式错误")
		}
		trades = append(trades, response.Data.List...)
		log.Printf("交易列表第 %d 页完成: recommendCode=%s, %d 条", pageNum, recommendCode, len(response.Data.List))
		if pageNum >= response.Data.Pages {
			break
		}
	}
	return trades, nil
}
