package main

import (
	"flag"
	"fmt"
)

func main() {
	a := flag.String("a","abc","abcd")
	flag.Parse()
	fmt.Println(*a)
}

