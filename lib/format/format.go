// Copyright 2020 Silvio Böhler
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

package format

import (
	"github.com/sboehler/knut/lib/model"
	"fmt"
	"io"
)

type iter interface {
	Next() (interface{}, error)
}

type directive interface {
	Position() model.Range
	io.WriterTo
}

type nextFunc func() (interface{}, error)

// Format formats the directives returned by p.
func Format(ch <-chan interface{}, src io.RuneReader, dest io.Writer) error {
	srcPos := 0
	for n := range ch {
		switch d := n.(type) {
		case error:
			if d == io.EOF {
				// make sure the file ends with a newline
				_, err := dest.Write([]byte(string('\n')))
				return err
			}
			return d

		case directive:
			// copy text before directive from src to dest
			for i := srcPos; i < d.Position().Start; i++ {
				r, _, err := src.ReadRune()
				if err != nil {
					return err
				}
				if _, err := dest.Write([]byte(string(r))); err != nil {
					return err
				}
			}
			// seek forward over directive in src
			for i := d.Position().Start; i < d.Position().End; i++ {
				if _, _, err := src.ReadRune(); err != nil {
					return err
				}
			}
			// write directive to dst
			if _, err := d.WriteTo(dest); err != nil {
				return err
			}
			// update srcPos
			srcPos = d.Position().End
		default:
			return fmt.Errorf("invalid directive: %v", d)
		}
	}
	return nil
}
