package poollib

import "sync"

type HandlerFunction func(vargs []interface{})
type Task struct {
	handler HandlerFunction
	params  []interface{}
}

type GoroutinePool struct {
	size                 int
	capacity             int
	request_goroutine_ch chan int
	wg                   *sync.WaitGroup
}

func (pool *GoroutinePool) NewGoroutinePool(num int) {
	pool.capacity = num
	pool.request_goroutine_ch = make(chan int, num)
	pool.wg = new(sync.WaitGroup)

	for i := 0; i < num; i++ {
		pool.request_goroutine_ch <- i
	}
}

func (pool *GoroutinePool) RunTask(function HandlerFunction, vargs []interface{}) {


	pool_id := <-pool.request_goroutine_ch
	pool.wg.Add(1)
	pool.size += 1

	go func () {
		function(vargs)
		defer func() {
			pool.wg.Done()
			pool.size -= 1
			pool.request_goroutine_ch <- pool_id
		}()
	}()
}

func (pool *GoroutinePool) WaitTask() {
	pool.wg.Wait()
}

func (pool *GoroutinePool) RenewTaskSize(size int) {
	pool.wg.Wait()
	pool.request_goroutine_ch = make(chan int,size)
	pool.size = size
	pool.capacity = size
}
