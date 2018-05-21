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

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	gat "google.golang.org/api/googleapi/transport"
	"google.golang.org/api/youtube/v3"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"github.com/orijtech/callback"
	"github.com/orijtech/otils"
	yt "github.com/orijtech/youtube"
)

type Search struct {
	client *yt.Client
}

type SearchInitOption interface {
	init(*Search)
}

type withClient struct {
	yc *yt.Client
}

var _ SearchInitOption = (*withClient)(nil)

func (wf *withClient) init(ss *Search) {
	ss.client = wf.yc
}

func WithYouTubeAPIKey(apiKey string) SearchInitOption {
	yc, err := yt.NewWithHTTPClient(&http.Client{
		Transport: &ochttp.Transport{Base: &gat.APIKey{Key: apiKey}},
	})
	if err != nil {
		log.Fatalf("WithYouTubeAPIKey: failed to create client, error: %v", err)
	}
	return &withClient{yc: yc}
}

var videoDetailingHTTPServerURL = otils.EnvOrAlternates("YOUTUBE_DETAILS_HTTP_SERVER_URL", "http://localhost:9944")

func NewSearch(opts ...SearchInitOption) (*Search, error) {
	ss := new(Search)
	for _, opt := range opts {
		opt.init(ss)
	}
	return ss, nil
}

var _ SearchServer = (*Search)(nil)

var youtubeSearches = stats.Int64("youtube_searches", "The number of YouTube searches", "1")
var youtubeAPIErrors = stats.Int64("youtube_api_errors", "The number of YouTube API errors", "1")

func (ss *Search) SearchIt(ctx context.Context, q *Query) (*SearchResults, error) {
	ctx, span := trace.StartSpan(ctx, "searchIt")
	defer span.End()

	// If blank or unset, ensure they are set
	q.setDefaultLimits()
	log.Printf("q: %v\n", q)

	ctx, err := tag.New(ctx, tag.Insert(tagKey("service"), "youtube-search"))
	if err != nil {
		return nil, err
	}
	stats.Record(ctx, youtubeSearches.M(1))

	pagesChan, err := ss.client.Search(ctx, &yt.SearchParam{
		Query:             q.Keywords,
		MaxPage:           uint64(q.MaxPages),
		MaxResultsPerPage: uint64(q.MaxResultsPerPage),
	})
	if err != nil {
		stats.Record(ctx, youtubeAPIErrors.M(1))
		span.Annotate([]trace.Attribute{
			trace.StringAttribute("api_error", err.Error()),
		}, "YouTube API search error")
		return nil, err
	}

	var srl []*SearchResult
	i := uint64(0)
	idListForDetails := make([]string, 0, 10)
	for page := range pagesChan {
		if len(page.Items) == 0 {
			continue
		}

		items := make([]*YouTubeResult, 0, len(page.Items))
		for _, item := range page.Items {
			if item != nil {
				sr := toSearchResult(item)
				items = append(items, sr)
				if sr.Id.VideoId != "" {
					idListForDetails = append(idListForDetails, sr.Id.VideoId)
				}
			}
		}

		if len(items) > 0 {
			i += 1
			srl = append(srl, &SearchResult{
				Items: items,
				Index: i,
			})
		}
	}

	if len(idListForDetails) > 0 && false {
		log.Printf("Firing off callback for %#v\n", idListForDetails)
		// Then fire off the callback to enable background
		// retrieval of detailed information of found videos.
		cb := callback.Callback{
			URL:     videoDetailingHTTPServerURL,
			Payload: idListForDetails,
		}
		cb.Do(ctx)
	}

	return &SearchResults{Results: srl}, nil
}

func tagKey(key string) tag.Key {
	k, _ := tag.NewKey(key)
	return k
}

func toSearchResult(yi *youtube.SearchResult) *YouTubeResult {
	return &YouTubeResult{
		Etag: yi.Etag,
		Kind: yi.Kind,
		Id: &YouTubeID{
			Kind:       yi.Id.Kind,
			VideoId:    yi.Id.VideoId,
			PlaylistId: yi.Id.PlaylistId,
		},
		Snippet: toSearchSnippet(yi.Snippet),
	}
}

func toSearchSnippet(snip *youtube.SearchResultSnippet) *YouTubeSnippet {
	return &YouTubeSnippet{
		ChannelTitle: snip.ChannelTitle,
		ChannelId:    snip.ChannelId,
		Description:  snip.Description,
		PublishedAt:  snip.PublishedAt,
		Title:        snip.Title,
		Thumbnails: map[string]*Thumbnail{
			"default":  toThumbnail(snip.Thumbnails.Default),
			"high":     toThumbnail(snip.Thumbnails.High),
			"maxres":   toThumbnail(snip.Thumbnails.Maxres),
			"medium":   toThumbnail(snip.Thumbnails.Medium),
			"standard": toThumbnail(snip.Thumbnails.Standard),
		},
	}
}

func toThumbnail(th *youtube.Thumbnail) *Thumbnail {
	if th == nil {
		return nil
	}
	return &Thumbnail{
		Height: th.Height,
		Width:  th.Width,
		Url:    th.Url,
	}
}

// And for HTTP based RPCs
func (ss *Search) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := trace.StartSpan(r.Context(), "/search")
	defer span.End()

	q, err := ExtractQuery(ctx, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	results, err := ss.SearchIt(ctx, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(results)
}

func ExtractQuery(ctx context.Context, r *http.Request) (*Query, error) {
	ctx, span := trace.StartSpan(ctx, "/extract-query")
	defer span.End()

	var body io.Reader

	switch r.Method {
	default:
		return nil, fmt.Errorf("Unacceptable method %q", r.Method)

	case "PUT", "POST":
		defer r.Body.Close()
		body = r.Body
		span.Annotate([]trace.Attribute{
			trace.StringAttribute("method", r.Method),
			trace.BoolAttribute("has_body", true),
		}, "Parsed a POST/PUT request")

		goto parseJSON

	case "GET":
		qv := r.URL.Query()
		outMap := make(map[string]string)
		for key := range qv {
			outMap[key] = qv.Get(key)
		}
		intermediateBlob, err := json.Marshal(outMap)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(intermediateBlob)
		span.Annotate([]trace.Attribute{
			trace.StringAttribute("method", "GET"),
			trace.BoolAttribute("has_body", false),
		}, "Parsed a GET request")

	}

parseJSON:
	_, span2 := trace.StartSpan(ctx, "/parse-json")
	defer span2.End()

	// By this point we are extracting only JSON.
	blob, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, err
	}
	qy := new(Query)
	if err := json.Unmarshal(blob, qy); err != nil {
		return nil, err
	}
	qy.setDefaultLimits()
	return qy, nil
}

func (q *Query) setDefaultLimits() {
	if q.MaxResultsPerPage <= 0 {
		q.MaxResultsPerPage = 10
	}
	if q.MaxPages <= 0 {
		q.MaxPages = 1
	}
}
