package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/QuangTung97/memproxy"
	"github.com/QuangTung97/memproxy/item"
	"github.com/jmoiron/sqlx"

	"bench-multiget/pb"
)

type CacheRepo struct {
	db     *sqlx.DB
	client memproxy.Memcache
}

func NewCacheRepo(db *sqlx.DB, client memproxy.Memcache) *CacheRepo {
	return &CacheRepo{
		db:     db,
		client: client,
	}
}

type ProductCacheKey struct {
	Sku string
}

func (k ProductCacheKey) String() string {
	return fmt.Sprintf("p/%s", k.Sku)
}

func getProductKey(p *pb.Product) ProductCacheKey {
	return ProductCacheKey{
		Sku: p.Sku,
	}
}

func mapSlice[A, B any](input []A, fn func(e A) B) []B {
	result := make([]B, 0, len(input))
	for _, e := range input {
		result = append(result, fn(e))
	}
	return result
}

type GetProductFunc = func() (CacheValue[*pb.Product], error)

type Stats struct {
	HitCount   atomic.Uint64
	MissCount  atomic.Uint64
	TotalBytes atomic.Uint64
}

func newProductProto() *pb.Product {
	return &pb.Product{}
}

type GetState = item.GetState[CacheValue[*pb.Product], ProductCacheKey]

func (r *CacheRepo) GetProducts(ctx context.Context, skus []string, globalStats *Stats) []*pb.Product {
	pipe := r.client.Pipeline(ctx)
	defer pipe.Finish()

	filler := item.NewMultiGetFiller[*pb.Product, ProductCacheKey](
		r.getProductsForCache, getProductKey,
	)
	productCache := NewCacheItem[*pb.Product, ProductCacheKey](pipe, newProductProto, filler)

	fnList := mapSlice(skus, func(sku string) *GetState {
		return productCache.GetFast(ctx, ProductCacheKey{
			Sku: sku,
		})
	})

	defer func() {
		stats := productCache.GetStats()
		globalStats.MissCount.Add(stats.FillCount)
		globalStats.HitCount.Add(stats.HitCount)
		globalStats.TotalBytes.Add(stats.TotalBytesRecv)
	}()

	return mapSlice(fnList, func(fn *GetState) *pb.Product {
		resp, err := fn.Result()
		if err != nil {
			panic(err)
		}
		return resp.Data
	})
}

type ProductContent struct {
	Sku     string `db:"sku"`
	Content []byte `db:"content"`
}

func (r *CacheRepo) getProductsForCache(ctx context.Context, keys []ProductCacheKey) ([]*pb.Product, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	skus := mapSlice(keys, func(k ProductCacheKey) string {
		return k.Sku
	})

	query := `
SELECT sku, content FROM products WHERE sku IN (?)
`
	query, args, err := sqlx.In(query, skus)
	if err != nil {
		return nil, err
	}

	var result []ProductContent
	err = r.db.SelectContext(ctx, &result, query, args...)
	if err != nil {
		return nil, err
	}
	return mapSlice(result, func(p ProductContent) *pb.Product {
		var product pb.Product
		err := json.Unmarshal(p.Content, &product)
		if err != nil {
			panic(err)
		}

		product.Sku = p.Sku
		return &product
	}), nil
}

func (r *CacheRepo) InsertProducts(ctx context.Context, products []*pb.Product) {
	contents := mapSlice(products, func(p *pb.Product) ProductContent {
		data, err := json.Marshal(p)
		if err != nil {
			panic(err)
		}
		return ProductContent{
			Sku:     p.Sku,
			Content: data,
		}
	})

	query := `
INSERT INTO products (sku, content)
VALUES (:sku, :content)
`
	_, err := r.db.NamedExecContext(ctx, query, contents)
	if err != nil {
		panic(err)
	}
}
