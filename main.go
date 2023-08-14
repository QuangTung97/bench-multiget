package main

import (
	"bench-multiget/pb"
	"context"
	"fmt"
	"github.com/QuangTung97/memproxy/proxy"
	"github.com/jmoiron/sqlx"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var createTableSQL = `
CREATE TABLE IF NOT EXISTS products (
    sku VARCHAR(100) NOT NULL PRIMARY KEY,
    content JSON NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)
`

func doMigrate(db *sqlx.DB) {
	db.MustExec(createTableSQL)
}

func repeatSlice[T any](e T, n int) []T {
	result := make([]T, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, e)
	}
	return result
}

func withIndex[T any](num int, fn func(i int) T) []T {
	result := make([]T, 0, num)
	for i := 0; i < num; i++ {
		result = append(result, fn(i))
	}
	return result
}

const numProducts = 10_000

func insertProducts(db *sqlx.DB) {
	repo := NewCacheRepo(db, nil)

	products := make([]*pb.Product, 0, numProducts)
	for i := 0; i < numProducts; i++ {
		products = append(products, &pb.Product{
			Sku:         fmt.Sprintf("SKU%07d", i+1),
			Name:        fmt.Sprintf("Product Name %d", i+1),
			DisplayName: fmt.Sprintf("Display Name %d", i+1),
			Desc:        fmt.Sprintf("Product Description %d", i+1),
			Attributes: repeatSlice(&pb.Attribute{
				Id:   int64(i + 1),
				Code: fmt.Sprintf("ATTR_CODE_%07d", i+1),
				Name: fmt.Sprintf("Attribute Name %d", i+1),
			}, 200),
			Brand: &pb.Brand{
				Id:   int64(i + 1),
				Code: fmt.Sprintf("BRAND_CODE_%07d", i+1),
				Name: fmt.Sprintf("Brand Name %d", i+1),
			},
		})
	}

	repo.InsertProducts(context.Background(), products)
}

func benchMultiGetFromCache(db *sqlx.DB) {
	servers := []proxy.SimpleServerConfig{
		{
			ID:   1,
			Host: "localhost",
			Port: 11211,
		},
	}

	statsClient := proxy.NewSimpleStats(servers)
	client, shutdownFunc, err := proxy.NewSimpleReplicatedMemcache(servers,
		4,
		statsClient,
	)
	if err != nil {
		panic(err)
	}
	defer shutdownFunc()

	repo := NewCacheRepo(db, client)

	allSkus := withIndex(numProducts, func(i int) string {
		return fmt.Sprintf("SKU%07d", i+1)
	})

	const numThreads = 10
	const numSkusPerBatch = 20
	const numBatches = numProducts / numSkusPerBatch

	const numLoops = 10_000

	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(numThreads)

	stats := Stats{}

	for th := 0; th < numThreads; th++ {
		go func() {
			defer wg.Done()

			for i := 0; i < numLoops; i++ {
				index := rand.Intn(numBatches) * numSkusPerBatch
				skus := allSkus[index : index+numSkusPerBatch]
				_ = repo.GetProducts(context.Background(), skus, &stats)
			}
		}()
	}

	wg.Wait()

	d := time.Since(start)
	fmt.Println("TOTAL TIME:", d)
	fmt.Println("BATCH SIZE:", numSkusPerBatch)
	fmt.Println("TOTAL THREADS:", numThreads)
	fmt.Println("TOTAL KEYS:", numThreads*numLoops*numSkusPerBatch)
	fmt.Println("TOTAL MISSES:", stats.MissCount.Load())
	fmt.Println("TOTAL HITS:", stats.HitCount.Load())
	fmt.Println("GETS per Second:", numThreads*numLoops*numSkusPerBatch/d.Seconds())
}

func benchMultiGetFromElastic(db *sqlx.DB) {
	repo := NewElasticRepo(db, "http://localhost:9200")

	allSkus := withIndex(numProducts, func(i int) string {
		return fmt.Sprintf("SKU%07d", i+1)
	})

	const numThreads = 10
	const numSkusPerBatch = 20
	const numBatches = numProducts / numSkusPerBatch

	const numLoops = 10_000

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(numThreads)

	var totalBytes atomic.Uint64

	for th := 0; th < numThreads; th++ {
		go func() {
			defer wg.Done()

			for i := 0; i < numLoops; i++ {
				index := rand.Intn(numBatches) * numSkusPerBatch
				skus := allSkus[index : index+numSkusPerBatch]
				_ = repo.GetProducts(skus, &totalBytes)
			}
		}()
	}
	wg.Wait()

	d := time.Since(start)
	fmt.Println("TOTAL TIME:", d)
	fmt.Println("TOTAL THREADS:", numThreads)
	fmt.Println("BATCH SIZE:", numSkusPerBatch)
	fmt.Println("TOTAL KEYS:", numThreads*numLoops*numSkusPerBatch)
	fmt.Println("TOTAL BYTES:", totalBytes.Load())
	fmt.Println("GETS per Second:", numThreads*numLoops*numSkusPerBatch/d.Seconds())
}

func main() {
	db := sqlx.MustConnect("mysql", "root:1@tcp(localhost:3306)/bench?parseTime=true")
	// doMigrate(db)
	// insertProducts(db)
	// benchMultiGetFromCache(db)
	// r.SyncProducts()
	benchMultiGetFromElastic(db)

	//var totalBytes atomic.Uint64
	//repo := NewElasticRepo(db, "http://localhost:9200")
	//products := repo.GetProducts([]string{"SKU0001350"}, &totalBytes)
	//fmt.Println(products[0])
}
