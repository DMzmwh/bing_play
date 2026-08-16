package concurrent

import (
	"changeme/internal/logger"
	"context"
	"runtime/debug"

	"golang.org/x/sync/semaphore"
)

type BufferedConcurrentChannel[T any] interface {
	In() chan<- T
	Run(context.Context, func(T)) error
}

type bufferedConcurrentChannel[T any] struct {
	ch  chan T
	sem *semaphore.Weighted
}

func NewBufferedConcurrentChannel[T any](capacity int, concurrency int) BufferedConcurrentChannel[T] {
	return &bufferedConcurrentChannel[T]{
		ch:  make(chan T, capacity),
		sem: semaphore.NewWeighted(int64(concurrency)),
	}
}

func (ch *bufferedConcurrentChannel[T]) In() chan<- T {
	return ch.ch
}

func (ch *bufferedConcurrentChannel[T]) Run(ctx context.Context, f func(T)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case next, ok := <-ch.ch:
			if !ok {
				return nil
			}
			if err := ch.sem.Acquire(ctx, 1); err != nil {
				return err
			}

			go func(item T) {
				defer ch.sem.Release(1)
				// 捕获panic，防止信号量永久占用
				defer func() {
					if r := recover(); r != nil {
						logger.App.Error(
							"concurrent task panic",
							"panic", r,
							"stack", string(debug.Stack()))
					}
				}()
				f(item)
			}(next)
		}
	}
}
