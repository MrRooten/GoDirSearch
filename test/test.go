package main

import (
	"fmt"
	"sync"
)

func main() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	i := new(int32)
	go func() {
		*i = 3
		wg.Done()
	}()
	wg.Wait()
	fmt.Println(*i)
}

