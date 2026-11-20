package internal

import (
	"encoding/json"
	"net/http"
	"time"
)

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/api/agents", agents)
	mux.HandleFunc("/api/agents/merged", mergedAgents)
	mux.HandleFunc("/api/agents/counts", agentCounts)
	return mux
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "请求方式错误")
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func agents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "请求方式错误")
		return
	}
	date, err := queryDate(r)
	if err != nil {
		writeError(w, "date 格式应为 YYYY-MM-DD")
		return
	}
	accounts, ok := queryAccounts(w, r)
	if !ok {
		return
	}
	page, err := QueryAgents(r.Context(), date, accounts)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(page)
}

func mergedAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "请求方式错误")
		return
	}
	date, err := queryDate(r)
	if err != nil {
		writeError(w, "date 格式应为 YYYY-MM-DD")
		return
	}
	accounts, ok := queryAccounts(w, r)
	if !ok {
		return
	}
	result, err := QueryMergedAgents(r.Context(), date, accounts)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

func agentCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "请求方式错误")
		return
	}
	date, err := queryDate(r)
	if err != nil {
		writeError(w, "date 格式应为 YYYY-MM-DD")
		return
	}
	accounts, ok := queryAccounts(w, r)
	if !ok {
		return
	}
	counts, err := QueryAgentCounts(r.Context(), date, accounts)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(counts)
}

func queryDate(r *http.Request) (string, error) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}
	_, err := time.Parse("2006-01-02", date)
	return date, err
}

func queryAccounts(w http.ResponseWriter, r *http.Request) (Config, bool) {
	client := r.URL.Query().Get("client")
	if client == "" {
		writeError(w, "client 参数不能为空")
		return Config{}, false
	}
	accounts, err := LoadClientAccounts(client)
	if err != nil {
		writeError(w, "客户配置不存在")
		return Config{}, false
	}
	return accounts, true
}

func writeError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(message))
}
