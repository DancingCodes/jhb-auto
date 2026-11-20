package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	publicKeyResponse, err := client.PublicKey(ctx)
	if err != nil {
		return nil, "", errors.New("登录失败")
	}
	var publicKey struct {
		Data struct {
			PublicKey string `json:"publicKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(publicKeyResponse, &publicKey); err != nil || publicKey.Data.PublicKey == "" {
		return nil, "", errors.New("登录失败")
	}
	credential, err := encryptPassword(publicKey.Data.PublicKey, account.Password)
	if err != nil {
		return nil, "", errors.New("登录失败")
	}
	loginResponse, err := client.Login(ctx, account.Username, credential)
	if err != nil {
		return nil, "", errors.New("登录失败")
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginResponse, &login); err != nil || login.Data.Token == "" {
		return nil, "", errors.New("登录失败")
	}
	return client, login.Data.Token, nil
}

func queryAgentHierarchy(ctx context.Context, client *Client, token, recommendCode string) (AgentResult, error) {
	var page AgentPage
	for pageNum := 1; ; pageNum++ {
		result, err := client.AgentList(ctx, token, recommendCode, pageNum)
		if err != nil {
			return AgentResult{}, errors.New("代理列表获取失败")
		}
		var response struct {
			Data *AgentPage `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			return AgentResult{}, errors.New("代理列表数据格式错误")
		}
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
		item := AgentWithDescendants{
			AgentWithData: AgentWithData{Agent: agent, Terminals: []Terminal{}},
			Descendants:   []AgentWithData{},
		}
		for pageNum := 1; ; pageNum++ {
			result, err := client.AgentDescendants(ctx, token, agent.ID, pageNum)
			if err != nil {
				return AgentResult{}, errors.New("代理下级获取失败")
			}
			var response struct {
				Data *AgentPage `json:"data"`
			}
			if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
				return AgentResult{}, errors.New("代理下级数据格式错误")
			}
			for _, descendant := range response.Data.List {
				item.Descendants = append(item.Descendants, AgentWithData{Agent: descendant, Terminals: []Terminal{}})
			}
			if pageNum >= response.Data.Pages {
				break
			}
		}
		agents.List = append(agents.List, item)
	}
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
		terminals, err := queryTerminals(ctx, client, token, agent.RecommendCode, date)
		if err != nil {
			return AgentResult{}, err
		}
		agent.Terminals = terminals
		for j := range agent.Descendants {
			descendant := &agent.Descendants[j]
			terminals, err := queryTerminals(ctx, client, token, descendant.RecommendCode, date)
			if err != nil {
				return AgentResult{}, err
			}
			descendant.Terminals = terminals
		}
	}
	for i := range agents.List {
		agent := &agents.List[i]
		if err := populateTerminalMerchants(ctx, client, token, agent.Terminals); err != nil {
			return AgentResult{}, err
		}
		for j := range agent.Descendants {
			descendant := &agent.Descendants[j]
			if err := populateTerminalMerchants(ctx, client, token, descendant.Terminals); err != nil {
				return AgentResult{}, err
			}
		}
	}
	for i := range agents.List {
		agent := &agents.List[i]
		trades, err := queryTrades(ctx, client, token, agent.RecommendCode, date)
		if err != nil {
			return AgentResult{}, err
		}
		agent.Trades = trades
		for j := range agent.Descendants {
			descendant := &agent.Descendants[j]
			trades, err := queryTrades(ctx, client, token, descendant.RecommendCode, date)
			if err != nil {
				return AgentResult{}, err
			}
			descendant.Trades = trades
		}
	}
	return agents, nil
}

type MergedAgentResult struct {
	Terminals []Terminal        `json:"auto_terminals"`
	Trades    []json.RawMessage `json:"auto_trades"`
}

func QueryMergedAgents(ctx context.Context, date string, config Config) (MergedAgentResult, error) {
	agents, err := QueryAgents(ctx, date, config)
	if err != nil {
		return MergedAgentResult{}, err
	}
	result := MergedAgentResult{Terminals: []Terminal{}, Trades: []json.RawMessage{}}
	for _, agent := range agents.List {
		result.Terminals = append(result.Terminals, agent.Terminals...)
		result.Trades = append(result.Trades, agent.Trades...)
		for _, descendant := range agent.Descendants {
			result.Terminals = append(result.Terminals, descendant.Terminals...)
			result.Trades = append(result.Trades, descendant.Trades...)
		}
	}
	return result, nil
}

func populateTerminalMerchants(ctx context.Context, client *Client, token string, terminals []Terminal) error {
	for _, terminal := range terminals {
		var sn string
		if err := json.Unmarshal(terminal["sn"], &sn); err != nil || sn == "" {
			return errors.New("终端 SN 数据格式错误")
		}
		merchants, err := queryMerchantsBySN(ctx, client, token, sn)
		if err != nil {
			return err
		}
		terminal["auto_merchants"], err = json.Marshal(merchants)
		if err != nil {
			return err
		}
	}
	return nil
}

func queryMerchantsBySN(ctx context.Context, client *Client, token, sn string) ([]Merchant, error) {
	merchants := []Merchant{}
	seenMerchants := make(map[string]bool)
	for pageNum := 1; ; pageNum++ {
		result, err := client.MerchantListBySN(ctx, token, sn, pageNum)
		if err != nil {
			return nil, fmt.Errorf("终端 %s 商户列表获取失败: %w", sn, err)
		}
		var response struct {
			Data *MerchantPage `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			return nil, fmt.Errorf("终端 %s 商户列表数据格式错误", sn)
		}
		for _, merchant := range response.Data.List {
			var merchantID string
			if err := json.Unmarshal(merchant["id"], &merchantID); err != nil || merchantID == "" {
				return nil, errors.New("商户列表数据格式错误")
			}
			if seenMerchants[merchantID] {
				continue
			}
			seenMerchants[merchantID] = true
			result, err := client.MerchantDetail(ctx, token, merchantID)
			if err != nil {
				return nil, fmt.Errorf("商户 %s 详情获取失败: %w", merchantID, err)
			}
			var detail struct {
				Data *json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(result, &detail); err != nil || detail.Data == nil {
				return nil, errors.New("商户详情数据格式错误")
			}
			merchant["auto_detail"] = *detail.Data
			merchants = append(merchants, merchant)
		}
		if pageNum >= response.Data.Pages {
			break
		}
	}
	return merchants, nil
}

func queryTerminals(ctx context.Context, client *Client, token, recommendCode, date string) ([]Terminal, error) {
	terminals := []Terminal{}
	startTime := date + " 00:00:00"
	endTime := date + " 23:59:59"
	for pageNum := 1; ; pageNum++ {
		result, err := client.TerminalList(ctx, token, recommendCode, startTime, endTime, pageNum)
		if err != nil {
			return nil, fmt.Errorf("代理 %s 终端列表获取失败: %w", recommendCode, err)
		}
		var response struct {
			Data *struct {
				Pages int        `json:"pages"`
				List  []Terminal `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			return nil, fmt.Errorf("代理 %s 终端列表数据格式错误", recommendCode)
		}
		terminals = append(terminals, response.Data.List...)
		if pageNum >= response.Data.Pages {
			break
		}
	}
	return terminals, nil
}

func queryTrades(ctx context.Context, client *Client, token, recommendCode, date string) ([]json.RawMessage, error) {
	trades := []json.RawMessage{}
	startTime := date + " 00:00:00"
	endTime := date + " 23:59:59"
	for pageNum := 1; ; pageNum++ {
		result, err := client.TradeList(ctx, token, recommendCode, startTime, endTime, pageNum)
		if err != nil {
			return nil, errors.New("交易列表获取失败")
		}
		var response struct {
			Data *TradePage `json:"data"`
		}
		if err := json.Unmarshal(result, &response); err != nil || response.Data == nil {
			return nil, errors.New("交易列表数据格式错误")
		}
		trades = append(trades, response.Data.List...)
		if pageNum >= response.Data.Pages {
			break
		}
	}
	return trades, nil
}
