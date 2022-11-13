package slice

import (
	"context"

	"github.com/sboehler/knut/lib/common/cpr"
	"golang.org/x/sync/errgroup"
)

func Adapt[T any](f func(t T) error) func(T, func(T)) error {
	return func(t T, next func(T)) error {
		if err := f(t); err != nil {
			return err
		}
		next(t)
		return nil
	}
}

const bufSize = 100

func Parallel[T any](ts []T, fs ...func(T, func(T)) error) ([]T, error) {
	wg, ctx := errgroup.WithContext(context.Background())
	firstCh := make(chan T, bufSize)
	ch := firstCh
	wg.Go(func() error {
		defer close(firstCh)
		for _, t := range ts {
			if err := cpr.Push(ctx, firstCh, t); err != nil {
				return err
			}
		}
		return nil
	})
	for _, f := range fs {
		inCh, f := ch, f
		outCh := make(chan T, bufSize)
		ch = outCh
		wg.Go(func() error {
			defer close(outCh)
			next := func(t T) {
				cpr.Push(ctx, outCh, t)
			}
			return cpr.Consume(ctx, inCh, func(t T) error {
				return f(t, next)
			})
		})
	}
	var res []T
	wg.Go(func() error {
		return cpr.Consume(ctx, ch, func(t T) error {
			res = append(res, t)
			return nil
		})
	})
	return res, wg.Wait()
}