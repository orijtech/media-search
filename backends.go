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
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/orijtech/media-search/rpc"
	"github.com/orijtech/otils"
)

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
		log.Fatal(http.ListenAndServe(":9988", mux))
	}()

	// And then set the trace config with the default sampler.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	view.SetReportingPeriod(10 * time.Second)
}

func main() {
	var onHTTP bool
	var port int
	flag.BoolVar(&onHTTP, "http", false, "if set true, run it as an HTTP server instead of as a gRPC server")
	flag.IntVar(&port, "port", 8899, "the port on which to run the server")
	flag.Parse()

	addr := fmt.Sprintf(":%d", port)

	// searchAPI handles both gRPC and HTTP transports.
	key := "YOUTUBE_API_KEY"
	envAPIKey := strings.TrimSpace(os.Getenv(key))
	if envAPIKey == "" {
		log.Fatalf("Failed to retrieve %q from environment", key)
	}
	if err := view.Register(ochttp.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register DefaultClientViews for YouTube client API's sake: %v", err)
	}

	searchAPI, err := rpc.NewSearch(rpc.WithYouTubeAPIKey(envAPIKey))
	if err != nil {
		log.Fatalf("Failed to create SearchAPI, error: %v", err)
	}
	genIDAPI, err := rpc.NewGenID()
	if err != nil {
		log.Fatalf("Failed to create GenIDAPI, error: %v", err)
	}

	switch onHTTP {
	case true:
		allViews := append(ochttp.DefaultServerViews, ochttp.DefaultClientViews...)
		if err := view.Register(allViews...); err != nil {
			log.Fatalf("Failed to register all HTTP views, error: %v", err)
		}
		mux := http.NewServeMux()
		mux.Handle("/search", searchAPI)
		mux.Handle("/id", genIDAPI)
		h := &ochttp.Handler{Handler: mux}

		if err := http.ListenAndServe(addr, h); err != nil {
			log.Fatalf("HTTP server ListenAndServe error: %v", err)
		}

	default:
		allViews := append(ocgrpc.DefaultServerViews, ocgrpc.DefaultClientViews...)
		if err := view.Register(allViews...); err != nil {
			log.Fatalf("Failed to register all gRPC views, error: %v", err)
		}

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("Failed to listen on  address %q error: %v", addr, err)
		}
		log.Printf("Serving as gRPC server at %q", addr)
		srv := grpc.NewServer(grpc.StatsHandler(&ocgrpc.ServerHandler{}))
		rpc.RegisterSearchServer(srv, searchAPI)
		rpc.RegisterGenIDServer(srv, genIDAPI)
		if err := srv.Serve(ln); err != nil {
			log.Fatalf("gRPC server Serve error: %v", err)
		}
	}
}
