package main

import (
	jhb "jhb-auto/internal"
	"net/http"
)

func main() {
	port := "9105"

	_ = http.ListenAndServe(":"+port, jhb.Handler())
}
