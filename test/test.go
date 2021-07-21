package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://baidu.com")
	fmt.Println(u.RawPath == "")
}

