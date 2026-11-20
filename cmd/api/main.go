package main

import (
	"fmt"
	jhb "jhb-auto/internal"
	"net/http"
)

func main() {
	port := "9105"

	fmt.Println("服务已启动：port:" + port)
	_ = http.ListenAndServe(":"+port, jhb.Handler())
}
