package main

import (
	"bench-multiget/pb"
	"context"
	"encoding/json"
	"fmt"
	"github.com/QuangTung97/memproxy"
	"github.com/QuangTung97/memproxy/item"
	"github.com/jmoiron/sqlx"
	"google.golang.org/protobuf/proto"
	"sync/atomic"
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

type ProductCacheValue struct {
	PB *pb.Product
}

func (p ProductCacheValue) GetKey() ProductCacheKey {
	return ProductCacheKey{
		Sku: p.PB.Sku,
	}
}

func (p ProductCacheValue) Marshal() ([]byte, error) {
	return proto.Marshal(p.PB)
}

func (k ProductCacheKey) String() string {
	return fmt.Sprintf("p/%s", k.Sku)
}

func unmarshalProduct(data []byte) (ProductCacheValue, error) {
	var p pb.Product
	err := proto.Unmarshal(data, &p)
	if err != nil {
		return ProductCacheValue{}, err
	}
	return ProductCacheValue{
		PB: &p,
	}, err
}

func mapSlice[A, B any](input []A, fn func(e A) B) []B {
	result := make([]B, 0, len(input))
	for _, e := range input {
		result = append(result, fn(e))
	}
	return result
}

type GetProductFunc = func() (ProductCacheValue, error)

type Stats struct {
	HitCount  atomic.Uint64
	MissCount atomic.Uint64
}

func (r *CacheRepo) GetProducts(ctx context.Context, skus []string, globalStats *Stats) []*pb.Product {
	pipe := r.client.Pipeline(ctx)
	defer pipe.Finish()

	filler := item.NewMultiGetFiller[ProductCacheValue, ProductCacheKey](
		r.getProductsForCache, ProductCacheValue.GetKey,
	)
	productCache := item.New[ProductCacheValue, ProductCacheKey](pipe, unmarshalProduct, filler)

	fnList := mapSlice(skus, func(sku string) GetProductFunc {
		return productCache.Get(ctx, ProductCacheKey{
			Sku: sku,
		})
	})

	defer func() {
		stats := productCache.GetStats()
		globalStats.MissCount.Add(stats.FillCount)
		globalStats.HitCount.Add(stats.HitCount)
	}()

	return mapSlice(fnList, func(fn GetProductFunc) *pb.Product {
		resp, err := fn()
		if err != nil {
			panic(err)
		}
		return resp.PB
	})
}

type ProductContent struct {
	Sku     string `db:"sku"`
	Content []byte `db:"content"`
}

func (r *CacheRepo) getProductsForCache(ctx context.Context, keys []ProductCacheKey) ([]ProductCacheValue, error) {
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
	return mapSlice(result, func(p ProductContent) ProductCacheValue {
		var product pb.Product
		err := json.Unmarshal(p.Content, &product)
		if err != nil {
			panic(err)
		}

		product.Sku = p.Sku
		return ProductCacheValue{
			PB: &product,
		}
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
