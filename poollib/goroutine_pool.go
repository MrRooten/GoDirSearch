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
	request_goroutine_ch chan bool
	wg                   *sync.WaitGroup
}

func (pool *GoroutinePool) NewGoroutinePool(num int) {
	pool.capacity = num
	pool.request_goroutine_ch = make(chan bool, num)
	pool.wg = &sync.WaitGroup{}

	for i := 0; i < num; i++ {
		pool.request_goroutine_ch <- true
	}
}

func (pool *GoroutinePool) RunTask(function HandlerFunction, vargs []interface{}) {


	<-pool.request_goroutine_ch
	pool.wg.Add(1)
	pool.size += 1

	func () {
		function(vargs)
	}()
		pool.wg.Done()
		pool.size -= 1
		pool.request_goroutine_ch <- true
}

func (pool *GoroutinePool) WaitTask() {
	pool.wg.Wait()
}

func (pool *GoroutinePool) RenewTaskSize(size int) {
	pool.wg.Wait()
	pool.request_goroutine_ch = make(chan bool,size)
	pool.size = size
	pool.capacity = size
}
