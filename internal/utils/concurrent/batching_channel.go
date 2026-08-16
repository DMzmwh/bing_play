package concurrent

import (
	"sync"
	"time"
)

type BatchingChannel[T any] interface {
	In() chan<- T
	Out() <-chan []T
	Close()
	// Wait 等待后台处理完全结束，Close之后调用，确保剩余数据输出完毕
	Wait()
}

type batchingChannel[T any] struct {
	input        chan T
	output       chan []T
	buffer       []T
	maxBatchSize int
	maxWaitTime  time.Duration

	closeOnce sync.Once
	done      chan struct{}
}

func NewBatchingChannel[T any](
	capacity int,
	outputCapacity int,
	maxBatchSize int,
	maxWaitTime time.Duration,
) BatchingChannel[T] {

	if maxBatchSize <= 0 {
		panic("maxBatchSize must be greater than 0")
	}
	if maxWaitTime <= 0 {
		panic("maxWaitTime must be greater than 0")
	}

	ch := &batchingChannel[T]{
		input:        make(chan T, capacity),
		output:       make(chan []T, outputCapacity),
		maxBatchSize: maxBatchSize,
		maxWaitTime:  maxWaitTime,
		done:         make(chan struct{}),
	}
	go ch.batchLoop()
	return ch
}

func (ch *batchingChannel[T]) In() chan<- T {
	return ch.input
}

func (ch *batchingChannel[T]) Out() <-chan []T {
	return ch.output
}

func (ch *batchingChannel[T]) Close() {
	ch.closeOnce.Do(func() {
		close(ch.input)
	})
}

func (ch *batchingChannel[T]) Wait() {
	<-ch.done
}

func (ch *batchingChannel[T]) batchLoop() {
	ticker := time.NewTicker(ch.maxWaitTime)
	defer func() {
		ticker.Stop()
		close(ch.output)
		close(ch.done)
	}()

	for {
		select {
		case item, ok := <-ch.input:
			if !ok {
				ch.flush()
				return
			}
			ch.buffer = append(ch.buffer, item)
			if len(ch.buffer) >= ch.maxBatchSize {
				ch.flush()
			}

		case <-ticker.C:
			if len(ch.buffer) > 0 {
				ch.flush()
			}
		}
	}
}

func (ch *batchingChannel[T]) flush() {
	if len(ch.buffer) == 0 {
		return
	}
	batch := ch.buffer
	ch.buffer = nil

	// 防止消费过慢卡死后台goroutine
	select {
	case ch.output <- batch:
	default:
		// output已满，这里策略：丢弃批次；业务如果不接受丢批，不能用default，就要上游限流
	}
}
