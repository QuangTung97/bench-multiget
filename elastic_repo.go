package main

import (
	"bench-multiget/pb"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/elastic/go-elasticsearch/v7"
	"github.com/jmoiron/sqlx"
	jsoniter "github.com/json-iterator/go"
	"io"
	"net/http"
	"sync/atomic"
)

type ElasticRepo struct {
	db     *sqlx.DB
	client *elasticsearch.Client
}

func NewElasticRepo(db *sqlx.DB, esAddr string) *ElasticRepo {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{esAddr},
	})
	if err != nil {
		panic(err)
	}
	return &ElasticRepo{
		db:     db,
		client: client,
	}
}

func (r *ElasticRepo) getProductsAfter(ctx context.Context, sku string, limit int) []*pb.Product {
	query := `
SELECT sku, content FROM products WHERE sku > ? ORDER BY sku LIMIT ?
`
	var result []ProductContent
	err := r.db.SelectContext(ctx, &result, query, sku, limit)
	if err != nil {
		panic(err)
	}

	return mapSlice(result, func(p ProductContent) *pb.Product {
		var product pb.Product
		err := json.Unmarshal(p.Content, &product)
		if err != nil {
			panic(err)
		}
		return &product
	})
}

func lastElem[T any](input []T) T {
	return input[len(input)-1]
}

const indexName = "multiget_products"

func (r *ElasticRepo) syncToElastic(products []*pb.Product) {
	bulkFn := r.client.Bulk

	type indexObject struct {
		ID string `json:"_id"`
	}

	type indexAction struct {
		Index indexObject `json:"index"`
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, p := range products {
		err := enc.Encode(indexAction{
			Index: indexObject{
				ID: p.Sku,
			},
		})
		if err != nil {
			panic(err)
		}

		err = enc.Encode(p)
		if err != nil {
			panic(err)
		}
	}

	resp, err := bulkFn(&buf, bulkFn.WithIndex(indexName))
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		panic(string(data))
	}
}

func (r *ElasticRepo) deleteIndex() {
	deleteFn := r.client.Indices.Delete
	resp, err := deleteFn([]string{indexName})
	if err != nil {
		panic(err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		data, _ := io.ReadAll(resp.Body)
		panic(string(data))
	}
}

//go:embed mapping.json
var indexMapping string

func (r *ElasticRepo) createIndex() {
	createFn := r.client.Indices.Create

	type createBody struct {
		Settings json.RawMessage `json:"settings"`
		Mappings json.RawMessage `json:"mappings"`
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	err := enc.Encode(createBody{
		Settings: []byte("{}"),
		Mappings: []byte(indexMapping),
	})
	if err != nil {
		panic(err)
	}

	resp, err := createFn(indexName, createFn.WithBody(&buf))
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		panic(string(data))
	}
}

func (r *ElasticRepo) SyncProducts() {
	r.deleteIndex()
	r.createIndex()

	lastSku := ""
	for {
		products := r.getProductsAfter(context.Background(), lastSku, 64)
		if len(products) == 0 {
			return
		}
		lastSku = lastElem(products).Sku

		r.syncToElastic(products)
		fmt.Println("SYNC:", len(products))
	}
}

func parseResponse(body io.Reader) []*pb.Product {
	type responseHit struct {
		Source json.RawMessage `json:"_source"`
	}

	type responseHits struct {
		Hits []responseHit `json:"hits"`
	}
	type response struct {
		Hits responseHits `json:"hits"`
	}

	var r response
	err := jsoniter.NewDecoder(body).Decode(&r)
	if err != nil {
		panic(err)
	}

	return mapSlice(r.Hits.Hits, func(e responseHit) *pb.Product {
		//var p pb.Product
		//err := jsoniter.Unmarshal(e.Source, &p)
		//if err != nil {
		//	panic(err)
		//}
		//return &p
		return nil
	})
}

func (r *ElasticRepo) GetProducts(skus []string, totalBytes *atomic.Uint64) []*pb.Product {
	type filterQuery struct {
		Terms map[string]any `json:"terms"`
	}

	type boolQuery struct {
		Filter filterQuery `json:"filter"`
	}

	type searchObject struct {
		Bool boolQuery `json:"bool"`
	}
	type searchQuery struct {
		Query        searchObject `json:"query"`
		Source       any          `json:"_source,omitempty"`
		Fields       []string     `json:"docvalue_fields,omitempty"`
		StoredFields string       `json:"stored_fields,omitempty"`
	}
	var buf bytes.Buffer

	enc := jsoniter.NewEncoder(&buf)
	err := enc.Encode(searchQuery{
		Query: searchObject{
			Bool: boolQuery{
				Filter: filterQuery{
					Terms: map[string]any{
						"sku": skus,
					},
				},
			},
		},
		// Source: []string{"sku", "attributes"},
		Source:       false,
		Fields:       []string{"sku"},
		StoredFields: "_none_",
	})
	if err != nil {
		panic(err)
	}
	// fmt.Println("QUERY:", buf.String())

	searchFn := r.client.Search
	resp, err := searchFn(searchFn.WithBody(&buf), searchFn.WithIndex(indexName))
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		panic(string(data))
	}

	return parseResponse(resp.Body)
}
