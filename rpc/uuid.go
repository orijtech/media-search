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
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/xid"
	"go.opencensus.io/trace"
)

type GID int

var _ GenIDServer = (*GID)(nil)

func NewGenID() (*GID, error) {
	return new(GID), nil
}

func (gi *GID) NewID(ctx context.Context, _ *Nothing) (*ID, error) {
	ctx, span := trace.StartSpan(ctx, "newID")
	defer span.End()

	return &ID{Value: xid.New().String()}, nil
}

func (gi *GID) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := gi.NewID(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enc := json.NewEncoder(w)
	enc.Encode(id)
}
