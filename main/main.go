package main

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
)

func main() {
	b := make([]byte, 20)
	crand.Read(b)
	fmt.Println(b)
	s := base64.URLEncoding.EncodeToString(b)

	fmt.Println(s)
}
