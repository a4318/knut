// Copyright 2021 Silvio Böhler
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package parser

import (
	"context"
	"io"
	"path"
	"path/filepath"
	"sync"

	"github.com/sboehler/knut/lib/common/cpr"
	"github.com/sboehler/knut/lib/journal"
)

// RecursiveParser parses a file hierarchy recursively.
type RecursiveParser struct {
	File    string
	Context journal.Context

	wg sync.WaitGroup
}

// Parse parses the journal at the path, and branches out for include files
func (rp *RecursiveParser) Parse(ctx context.Context) <-chan any {
	resCh := make(chan any, 1000)

	rp.wg.Add(1)
	go func() {
		defer rp.wg.Done()
		err := rp.parseRecursively(ctx, resCh, rp.File)
		if err != nil && ctx.Err() == nil {
			cpr.Push[any](ctx, resCh, err)
		}
	}()

	// Parse and eventually close input channel
	go func() {
		defer close(resCh)
		rp.wg.Wait()
	}()
	return resCh
}

func (rp *RecursiveParser) parseRecursively(ctx context.Context, resCh chan<- any, file string) error {
	p, cls, err := FromPath(rp.Context, file)
	if err != nil {
		return err
	}
	defer cls()

	for {
		d, err := p.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := d.(type) {
		case *journal.Include:
			rp.wg.Add(1)
			go func() {
				defer rp.wg.Done()
				err := rp.parseRecursively(ctx, resCh, path.Join(filepath.Dir(file), t.Path))
				if err != nil && ctx.Err() == nil {
					cpr.Push[any](ctx, resCh, err)
				}
			}()
		default:
			if err := cpr.Push[any](ctx, resCh, d); err != nil {
				return err
			}
		}
	}
}