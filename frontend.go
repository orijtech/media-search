// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc"

	"github.com/gomodule/redigo/redis"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"go.opencensus.io/zpages"

	"github.com/orijtech/media-search/rpc"
	"github.com/orijtech/otils"
)

var genIDClient rpc.GenIDClient
var searchClient rpc.SearchClient

// The Redis pool
var redisPool = &redis.Pool{
	MaxIdle:     5,
	IdleTimeout: 5 * time.Minute,
	Dial: func() (redis.Conn, error) {
		return redis.Dial("tcp", otils.EnvOrAlternates("REDIS_SERVER_URI", "localhost:6379"))
	},
}

func newRedisConn(ctx context.Context) redis.Conn {
	return redisPool.GetWithContext(ctx)
}

func main() {
	// Firstly dial to the search service
	searchAddr := ":8899"
	conn, err := grpc.Dial(searchAddr, grpc.WithInsecure(), grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
	if err != nil {
		log.Fatalf("Failed to dial to gRPC server: %v", err)
	}
	searchClient = rpc.NewSearchClient(conn)
	genIDClient = rpc.NewGenIDClient(conn)
	log.Printf("Successfully dialed to the gRPC {id, search} services at %q", searchAddr)

	// Subscribe to every view available since the service is a mix of gRPC and HTTP, client and server services.
	allViews := append(ochttp.DefaultClientViews, ochttp.DefaultServerViews...)
	allViews = append(allViews, ocgrpc.DefaultClientViews...)
	allViews = append(allViews, ocgrpc.DefaultServerViews...)
	if err := view.Register(allViews...); err != nil {
		log.Fatalf("Failed to register all the default {ocgrpc, ochttp} views: %v", err)
	}

	// Create and register the exporters
	se, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:    otils.EnvOrAlternates("OPENCENSUS_GCP_PROJECTID", "census-demos"),
		MetricPrefix: "gosf",
	})
	if err != nil {
		log.Fatalf("Stackdriver newExporter error: %v", err)
	}

	// AWS X-Ray
	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("Failed to register AWS X-Ray exporter: %v", err)
	}

	// zpages
	go func() {
		mux := http.NewServeMux()
		zpages.Handle(mux, "/debug")
		if err := http.ListenAndServe(":7788", mux); err != nil {
			log.Fatalf("Failed to serve zPages: %v", err)
		}
	}()

	// Now register the exporters
	trace.RegisterExporter(se)
	trace.RegisterExporter(xe)
	view.RegisterExporter(se)

	view.SetReportingPeriod(90 * time.Second)

	// Register the views from Redis Go driver
	if err := view.Register(redis.ObservabilityMetricViews...); err != nil {
		log.Fatalf("Failed to register Redis views: %v", err)
	}

	// Set the sampling rate
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	// And then for the custom views
	err = view.Register([]*view.View{
		{Name: "cache_hits", Description: "cache hits", Measure: cacheHits, Aggregation: view.Count()},
		{Name: "cache_misses", Description: "cache misses", Measure: cacheMisses, Aggregation: view.Count()},
		{
			Name: "cache_insertion_errors", Description: "cache insertion errors",
			Measure: cacheInsertionErrors, Aggregation: view.Count(), TagKeys: []tag.Key{mustKey("cache_errors")},
		}, {

			Name: "youtube_api_errors", Description: "youtube errors",
			Measure: youtubeAPIErrors, Aggregation: view.Count(),
			TagKeys: []tag.Key{mustKey("api"), mustKey("youtube_api")},
		}, {
			Name: "redis_errors", Description: "Redis errors",
			Measure: redisErrors, Aggregation: view.Count(),
			TagKeys: []tag.Key{mustKey("api"), mustKey("redis")},
		},
	}...)
	if err != nil {
		log.Fatalf("Failed to register custom views: %v", err)
	}

	log.Printf("Successfully finished exporter and view registration")

	// Now create the server
	addr := ":9778"
	mux := http.NewServeMux()
	mux.HandleFunc("/search", search)
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	h := &ochttp.Handler{
		// Wrap the handler with CORS
		Handler: otils.CORSMiddlewareAllInclusive(mux),
	}
	log.Printf("Serving on %q", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatalf("ListenAndServe err: %v", err)
	}
}

type dbCacheKV struct {
	CacheID   string    `json:"cache_id" bson:"cache_id,omitempty"`
	Key       string    `json:"key" bson:"key,omitempty"`
	Value     []byte    `json:"value" bson:"value,omitempty"`
	CacheTime time.Time `json:"ct" bson:"ct,omitempty"`
}

var rpcNothing = new(rpc.Nothing)

func search(w http.ResponseWriter, r *http.Request) {
	sc := trace.FromContext(r.Context()).SpanContext()
	log.Printf("search here: %+v\n", sc)
	ctx, span := trace.StartSpan(r.Context(), "/search")
	defer span.End()

	if r.Method == "OPTIONS" {
		return
	}

	q, err := rpc.ExtractQuery(ctx, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	keywords := q.Keywords
	if keywords == "" {
		http.Error(w, "Expecting keywords", http.StatusBadRequest)
		return
	}
	cacheKey := q.Keywords

	span.Annotate([]trace.Attribute{
		trace.StringAttribute("db", "mongodb"),
		trace.StringAttribute("driver", "go"),
	}, "Checking cache if the query is present")

	redisConn := newRedisConn(ctx)
	defer redisConn.Close()

	data, err := redisConn.Do("GET", cacheKey)
	// 1. Firstly check if this has been cached before
	if err == nil && data != nil {
		dt := data.([]byte)
		if len(dt) > 1 {
			span.Annotate([]trace.Attribute{
				trace.BoolAttribute("hit", true),
				trace.StringAttribute("db", "redis"),
				trace.StringAttribute("driver", "go"),
			}, "Cache hit")
			stats.Record(ctx, cacheHits.M(1))

			w.Write(dt)
			return
		}
	} else if err != nil {
		span.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		stats.Record(ctx, redisErrors.M(1))
                // But we shouldn't exist ASAP!
		// http.Error(w, err.Error(), http.StatusBadRequest)
		// return
	}

	// 2. Otherwise that was a cache-miss, now retrieve it then save it
	stats.Record(ctx, cacheMisses.M(1))

	// 3. Get the global CacheID
	if _, err = genIDClient.NewID(ctx, rpcNothing); err != nil {
		span.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	span.Annotate([]trace.Attribute{
		trace.BoolAttribute("hit", false),
		trace.StringAttribute("db", "redis"),
		trace.StringAttribute("driver", "go"),
	}, "Cache miss, hence YouTube API search")

	results, err := searchClient.SearchIt(ctx, q)
	if err != nil {
		stats.Record(ctx, youtubeAPIErrors.M(1))
		span.Annotate([]trace.Attribute{
			trace.StringAttribute("api_error", err.Error()),
		}, "YouTube API search error")
		span.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	outBlob, err := json.Marshal(results.Results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Now cache it so that next time it'll be a hit.
	if _, err := redisConn.Do("SETEX", cacheKey, _3HoursInSeconds, outBlob); err != nil {
		stats.Record(ctx, cacheInsertionErrors.M(1))
	}

	_, _ = w.Write(outBlob)
}

const _3HoursInSeconds = 3 * 60 * 60

var (
	cacheHits   = stats.Int64("cache_hits", "the number of cache hits", stats.UnitNone)
	cacheMisses = stats.Int64("cache_misses", "the number of cache misses", stats.UnitNone)

	cacheInsertionErrors = stats.Int64("cache_insertion_errors", "the number of cache insertion errors", stats.UnitNone)

	youtubeAPIErrors = stats.Int64("youtube_api_errors", "the number of youtube API lookup errors", stats.UnitNone)
	redisErrors      = stats.Int64("redis_errors", "the number of Redis errors", stats.UnitNone)

	blankDBKV = new(dbCacheKV)
)

func mustKey(sk string) tag.Key {
	k, err := tag.NewKey(sk)
	if err != nil {
		log.Fatalf("Creating new key %q error: %v", sk, err)
	}
	return k
}
