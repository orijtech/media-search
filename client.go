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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/orijtech/otils"
	"github.com/orijtech/youtube"
)

func init() {
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("X-Ray newExporter: %v", err)
	}
	trace.RegisterExporter(xe)
	se, err := stackdriver.NewExporter(stackdriver.Options{ProjectID: otils.EnvOrAlternates("OPENCENSUS_GCP_PROJECTID", "census-demos")})
	if err != nil {
		log.Fatalf("Stackdriver newExporter: %v", err)
	}
	trace.RegisterExporter(se)
	view.RegisterExporter(se)
	if err := view.Register(ochttp.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register views: %v", err)
	}
}

func main() {
	client := &http.Client{}
	br := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Content to search$ ")
		input, _, err := br.ReadLine()
		if err != nil {
			log.Fatalf("Failed to read input: %v", err)
		}
		inBlob, err := json.Marshal(map[string]string{
			"q": string(input),
		})
		if err != nil {
			log.Fatalf("Failed to json.Marshal input blob: %v", err)
		}
		req, err := http.NewRequest("POST", "http://localhost:9778/search", bytes.NewReader(inBlob))
		if err != nil {
			log.Fatalf("Failed to build POST request: %v", err)
		}
		res, err := client.Do(req)
		if err != nil {
			log.Fatalf("Failed to POST: %v", err)
		}
		outBlob, err := ioutil.ReadAll(res.Body)
		_ = res.Body.Close()
		if !otils.StatusOK(res.StatusCode) {
			log.Printf("Error encountered: statusCode: %d message: %s", res.StatusCode, outBlob)
			continue
		}
		if err != nil {
			log.Fatalf("Failed to read res.Body: %v", err)
		}
		var pages []*youtube.SearchPage
		if err := json.Unmarshal(outBlob, &pages); err != nil {
			log.Fatalf("Unmarshaling responses: %v", err)
		}
		for _, page := range pages {
			for _, video := range page.Items {
				if video == nil {
					continue
				}
				snippet := video.Snippet
				if video.Id.VideoId != "" {
					fmt.Printf("URL: https://youtu.be/%s\n", video.Id.VideoId)
				} else if video.Id.ChannelId != "" {
					fmt.Printf("ChannelURL: https://www.youtube.com/channel/%s\n",
						video.Id.ChannelId)
				}
				fmt.Printf("Title: %s\nDescription: %s\n\n\n", snippet.Title, snippet.Description)
			}
		}
	}
}
