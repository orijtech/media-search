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
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	gat "google.golang.org/api/googleapi/transport"
	"google.golang.org/api/youtube/v3"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/mongodb/mongo-go-driver/bson"
	"github.com/mongodb/mongo-go-driver/mongo"

	"github.com/orijtech/otils"
	yt "github.com/orijtech/youtube"
)

var ytDetailsCollection *mongo.Collection
var yc *yt.Client

func init() {
	// Log into MongoDB
	mongoServerURI := otils.EnvOrAlternates("MEDIA_SEARCH_MONGO_SERVER_URI", "localhost:27017")
	mongoClient, err := mongo.NewClient("mongodb://" + mongoServerURI)
	log.Printf("mongoServerURI: %q\n", mongoServerURI)
	if err != nil {
		log.Fatalf("Failed to log into Mongo error: %v", err)
	}
	// Create or get the details collection.
	ytDetailsCollection = mongoClient.Database("media-searches").Collection("youtube_details")

	envAPIKey := otils.EnvOrAlternates("YOUTUBE_API_KEY", "AIzaSyCokXpH0NP3MGqaoEFSshet8YGbsOP0lFE")
	yc, err = yt.NewWithHTTPClient(&http.Client{
		Transport: &ochttp.Transport{Base: &gat.APIKey{Key: envAPIKey}},
	})
	if err != nil {
		log.Fatalf("Creating YouTube client error: %v", err)
	}

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

	// Configure the tracer
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	view.SetReportingPeriod(10 * time.Second)

	// Serve the Prometheus metrics
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		log.Fatal(http.ListenAndServe(":9989", mux))
	}()

	// And then set the trace config with the default sampler.
	view.SetReportingPeriod(15 * time.Second)
}

func main() {
	var port int
	flag.IntVar(&port, "port", 9944, "the port to run the server on")
	flag.Parse()

	addr := fmt.Sprintf(":%d", port)
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleDetailing)
	h := &ochttp.Handler{Handler: mux}

	log.Printf("Serving on %q", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatalf("Failed to serve the detailing server: %v", err)
	}
}

func handleDetailing(w http.ResponseWriter, r *http.Request) {
	_, span := trace.StartSpan(r.Context(), "/youtube-detailing")
	defer span.End()

	if r.Method != "POST" {
		http.Error(w, fmt.Sprintf(`only accepting "POST" not %q`, r.Method), http.StatusMethodNotAllowed)
		return
	}

	// Detailing looks up YouTube IDs by ID and then updates MongoDB
	// with the entry to ensure that later information lookup e.g. on Mobile
	// is fast and seamless for scrolling and search indexing.
	var idList []string
	dec := json.NewDecoder(r.Body)
	defer r.Body.Close()

	if err := dec.Decode(&idList); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// We don't care too much about the result, we
	// just need to fire off this callback so that
	// whenever videos can be detailed in the background,
	// then they will be detailed.
	go performDetailing(idList)
}

func performDetailing(idList []string) {
	ctx, span := trace.StartSpan(context.Background(), "detailing")
	defer span.End()

	videos, err := lookupAndSetYouTubeDetails(ctx, idList)
	if err != nil {
		log.Printf("Detailing error: %v idList=%#v", err, idList)
		return
	}

	for _, video := range videos {
		if video == nil {
			continue
		}
		filter := bson.NewDocument(bson.EC.String("yt_id", video.Id))
		_, _ = ytDetailsCollection.UpdateOne(ctx, filter, video)
	}
}

var errNotFound = errors.New("no details found for video")

func lookupAndSetYouTubeDetails(ctx context.Context, youtubeIDs []string) ([]*youtube.Video, error) {
	log.Printf("Got requests: %#v\n", youtubeIDs)
	ctx, span := trace.StartSpan(ctx, "lookup-and-set-details")
	defer span.End()

	videoPages, err := yc.ById(ctx, youtubeIDs...)
	if err != nil {
		return nil, err
	}

	var detailsList []*youtube.Video
	for page := range videoPages {
		if page.Err != nil {
			continue
		}

		for _, item := range page.Items {
			if item != nil {
				detailsList = append(detailsList, item)
			}
		}
	}

	if len(detailsList) == 0 {
		return nil, errNotFound
	}

	return detailsList, nil
}
