package main

import (
	"context"

	"github.com/QuangTung97/memproxy"
	"github.com/QuangTung97/memproxy/item"
)

type ProtoMessage interface {
	Marshal() ([]byte, error)
	Unmarshal(data []byte) error
}

type CacheValue[T ProtoMessage] struct {
	Data T
}

func (p CacheValue[T]) Marshal() ([]byte, error) {
	return p.Data.Marshal()
}

func unmarshalCacheValue[T ProtoMessage](newFunc func() T) func(data []byte) (CacheValue[T], error) {
	return func(data []byte) (CacheValue[T], error) {
		v := CacheValue[T]{
			Data: newFunc(),
		}
		err := v.Data.Unmarshal(data)
		return v, err
	}
}

type Item[T ProtoMessage, K item.Key] struct {
	item.Item[CacheValue[T], K]
}

func NewCacheItem[T ProtoMessage, K item.Key](
	pipe memproxy.Pipeline, newFunc func() T,
	filler func(ctx context.Context, key K) func() (T, error),
) *Item[T, K] {
	it := item.New[CacheValue[T], K](
		pipe,
		unmarshalCacheValue[T](newFunc),
		func(ctx context.Context, key K) func() (CacheValue[T], error) {
			fn := filler(ctx, key)
			return func() (CacheValue[T], error) {
				resp, err := fn()
				if err != nil {
					return CacheValue[T]{}, err
				}
				return CacheValue[T]{
					Data: resp,
				}, nil
			}
		},
	)
	return &Item[T, K]{
		Item: *it,
	}
}
