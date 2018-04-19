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
	"encoding/json"
	"log"
	"net/http"
	"reflect"
	"time"

	"google.golang.org/grpc"

	"github.com/mongodb/mongo-go-driver/bson"
	"github.com/mongodb/mongo-go-driver/mongo"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"github.com/orijtech/media-search/rpc"
	"github.com/orijtech/otils"
)

var ytSearchesCollection *mongo.Collection
var genIDClient rpc.GenIDClient
var searchClient rpc.SearchClient

func init() {
	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("X-Ray newExporter: %v", err)
	}
	se, err := stackdriver.NewExporter(stackdriver.Options{ProjectID: otils.EnvOrAlternates("OPENCENSUS_GCP_PROJECTID", "census-demos")})
	if err != nil {
		log.Fatalf("Stackdriver newExporter: %v", err)
	}
	pe, err := prometheus.NewExporter(prometheus.Options{Namespace: "mediasearch"})
	if err != nil {
		log.Fatalf("Prometheus newExporter: %v", err)
	}

	// Now register the exporters
	trace.RegisterExporter(xe)
	trace.RegisterExporter(se)
	view.RegisterExporter(se)
	view.RegisterExporter(pe)

	// Serve the Prometheus metrics
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		log.Fatal(http.ListenAndServe(":9888", mux))
	}()

	// And then set the trace config with the default sampler.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	view.SetReportingPeriod(10 * time.Second)

	mustKey := func(sk string) tag.Key {
		k, err := tag.NewKey(sk)
		if err != nil {
			log.Fatalf("Creating new key %q error: %v", sk, err)
		}
		return k
	}

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
			Name: "mongo_errors", Description: "MongoDB errors",
			Measure: mongoErrors, Aggregation: view.Count(),
			TagKeys: []tag.Key{mustKey("api"), mustKey("mongo")},
		},
	}...)
	if err != nil {
		log.Fatalf("Failed to register custom views: %v", err)
	}

	log.Printf("Successfully finished exporter and view registration")

	// Log into MongoDB
	mongoServerURI := otils.EnvOrAlternates("MEDIA_SEARCH_MONGO_SERVER_URI", "localhost:27017")
	mongoClient, err := mongo.NewClient("mongodb://" + mongoServerURI)
	log.Printf("mongoServerURI: %q\n", mongoServerURI)
	if err != nil {
		log.Fatalf("Failed to log into Mongo error: %v", err)
	}
	// Create or get the searches collection.
	ytSearchesCollection = mongoClient.Database("media-searches").Collection("youtube_searches")
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

	q, err := rpc.ExtractQuery(ctx, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	keywords := q.Keywords
	filter := bson.NewDocument(bson.EC.String("key", q.Keywords))

	span.Annotate([]trace.Attribute{
		trace.StringAttribute("db", "mongodb"),
		trace.StringAttribute("driver", "go"),
	}, "Checking cache if the query is present")

	dbRes := ytSearchesCollection.FindOne(ctx, filter)
	// 1. Firstly check if this has been cached before
	cachedKV := new(dbCacheKV)

	switch err := dbRes.Decode(cachedKV); err {
	default:
		stats.Record(ctx, mongoErrors.M(1))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return

	case nil: // Cache hit!
		if !reflect.DeepEqual(cachedKV, blankDBKV) {
			span.Annotate([]trace.Attribute{
				trace.BoolAttribute("hit", true),
				trace.StringAttribute("db", "mongodb"),
				trace.StringAttribute("driver", "go"),
			}, "Cache hit")
			stats.Record(ctx, cacheHits.M(1))
			w.Write(cachedKV.Value)
			return
		}

		// Otherwise this is false cache hit!

	case bson.ErrElementNotFound, mongo.ErrNoDocuments:
		// Cache miss, now retrieve the results below
	}

	// 2. Otherwise that was a cache-miss, now retrieve it then save it
	stats.Record(ctx, cacheMisses.M(1))

	// 3. Get the global CacheID
	cacheID, err := genIDClient.NewID(ctx, rpcNothing)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	span.Annotate([]trace.Attribute{
		trace.BoolAttribute("hit", false),
		trace.StringAttribute("db", "mongodb"),
		trace.StringAttribute("driver", "go"),
	}, "Cache miss, hence YouTube API search")

	results, err := searchClient.SearchIt(ctx, q)
	if err != nil {
		stats.Record(ctx, youtubeAPIErrors.M(1))
		span.Annotate([]trace.Attribute{
			trace.StringAttribute("api_error", err.Error()),
			trace.StringAttribute("db", "mongodb"),
			trace.StringAttribute("driver", "go"),
		}, "YouTube API search error")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	outBlob, err := json.Marshal(results.Results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Now cache it so that next time it'll be a hit.
	insertKV := &dbCacheKV{
		CacheID:   cacheID.Value,
		Key:       keywords,
		Value:     outBlob,
		CacheTime: time.Now(),
	}

	if _, err := ytSearchesCollection.InsertOne(ctx, insertKV); err != nil {
		stats.Record(ctx, cacheInsertionErrors.M(1))
	}

	_, _ = w.Write(outBlob)
}

var (
	cacheHits   = stats.Int64("cache_hits", "the number of cache hits", stats.UnitNone)
	cacheMisses = stats.Int64("cache_misses", "the number of cache misses", stats.UnitNone)

	cacheInsertionErrors = stats.Int64("cache_insertion_errors", "the number of cache insertion errors", stats.UnitNone)

	youtubeAPIErrors = stats.Int64("youtube_api_errors", "the number of youtube API lookup errors", stats.UnitNone)
	mongoErrors      = stats.Int64("mongo_errors", "the number of MongoDB errors", stats.UnitNone)

	blankDBKV = new(dbCacheKV)
)
