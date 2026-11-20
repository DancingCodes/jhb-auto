package internal

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/api/agents/merged", mergedAgents)
	return mux
}

func health(w http.ResponseWriter, r *http.Request) {
	finish := beginRequest(r)
	defer finish()
	if r.Method != http.MethodGet {
		writeError(w, "请求方式错误")
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func mergedAgents(w http.ResponseWriter, r *http.Request) {
	finish := beginRequest(r)
	defer finish()
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

func beginRequest(r *http.Request) func() {
	start := time.Now()
	log.Printf("接口开始: method=%s path=%s start=%s", r.Method, r.URL.Path, start.Format(time.RFC3339))
	return func() {
		end := time.Now()
		log.Printf("接口结束: method=%s path=%s end=%s 累计用时=%s", r.Method, r.URL.Path, end.Format(time.RFC3339), end.Sub(start))
	}
}
