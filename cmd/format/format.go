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
	"bufio"
	"io"
	"io/ioutil"
	"os"
	"path"

	"github.com/spf13/cobra"
	"go.uber.org/multierr"

	"github.com/sboehler/knut/lib/format"
	"github.com/sboehler/knut/lib/model"
	"github.com/sboehler/knut/lib/parser"
)

// Cmd is the import command.
var Cmd = &cobra.Command{
	Use:   "format",
	Short: "Format the given journal",
	Long:  `Format the given journal in-place. Any white space and comments between directives is preserved.`,

	RunE: run,
}

type directive interface {
	io.WriterTo
	Position() model.Range
}

const concurrency = 10

func run(cmd *cobra.Command, args []string) (errors error) {
	errCh := make(chan error)
	defer close(errCh)
	go func() {
		for err := range errCh {
			errors = multierr.Append(errors, err)
		}
	}()
	sema := make(chan bool, concurrency)
	for _, arg := range args {
		sema <- true
		go func(target string) {
			defer func() { <-sema }()
			if err := formatFile(target); err != nil {
				errCh <- err
			}
		}(arg)
	}
	for i := 0; i < concurrency; i++ {
		sema <- true
	}
	return nil
}

func formatFile(target string) error {
	ch, err := parser.ParseOneFile(target)
	if err != nil {
		return err
	}
	srcFile, err := os.Open(target)
	if err != nil {
		return err
	}
	src := bufio.NewReader(srcFile)
	tmpfile, err := ioutil.TempFile(path.Dir(target), "-format")
	if err != nil {
		return err
	}
	dest := bufio.NewWriter(tmpfile)
	err = format.Format(ch, src, dest)
	if err = multierr.Combine(err, dest.Flush(), srcFile.Close()); err != nil {
		return multierr.Append(err, os.Remove(tmpfile.Name()))
	}
	return os.Rename(tmpfile.Name(), target)
}
