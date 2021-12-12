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

// Package api provides the knut web API.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/sboehler/knut/lib/balance"
	"github.com/sboehler/knut/lib/date"
	"github.com/sboehler/knut/lib/ledger"
	"github.com/sboehler/knut/lib/parser"
	"github.com/shopspring/decimal"
)

// New instantiates the API handler.
func New(file string) http.Handler {
	var s = http.NewServeMux()
	s.Handle("/balance", handler{file})
	return s
}

// handler handles HTTP.
type handler struct {
	File string
}

func (s handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		ppl *pipeline
		err error
	)
	if ppl, err = buildPipeline(s.File, r.URL.Query()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err = ppl.process(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type pipeline struct {
	Accounts        *ledger.Accounts
	Parser          parser.RecursiveParser
	Filter          ledger.Filter
	ProcessingSteps []ledger.Processor
	Balances        *[]*balance.Balance
}

func buildPipeline(file string, query url.Values) (*pipeline, error) {
	var (
		ctx                               = ledger.NewContext()
		period                            date.Period
		commoditiesFilter, accountsFilter *regexp.Regexp
		from, to                          time.Time
		last                              int
		valuation                         *ledger.Commodity
		diff                              bool
		err                               error
	)
	if period, err = parsePeriod(query, "period"); err != nil {
		return nil, err
	}
	if commoditiesFilter, err = parseRegex(query, "commodity"); err != nil {
		return nil, err
	}
	if accountsFilter, err = parseRegex(query, "account"); err != nil {
		return nil, err
	}
	if from, err = parseDate(query, "from"); err != nil {
		return nil, err
	}
	if to, err = parseDate(query, "to"); err != nil {
		return nil, err
	}
	if last, err = parseInt(query, "last"); err != nil {
		return nil, err
	}
	if valuation, err = parseCommodity(query, ctx, "valuation"); err != nil {
		return nil, err
	}
	if diff, err = parseBool(query, "diff"); err != nil {
		return nil, err
	}

	var (
		bal    = balance.New(ctx, valuation)
		result []*balance.Balance
		steps  = []ledger.Processor{
			balance.DateUpdater{Balance: bal},
			&balance.Snapshotter{
				Balance: bal,
				From:    from,
				To:      to,
				Period:  period,
				Last:    last,
				Diff:    diff,
				//TODO: implement result with a channel
				//Result:  &result
			},
			balance.AccountOpener{Balance: bal},
			balance.TransactionBooker{Balance: bal},
			balance.ValueBooker{Balance: bal},
			balance.Asserter{Balance: bal},
			&balance.PriceUpdater{Balance: bal},
			balance.TransactionValuator{Balance: bal},
			balance.ValuationTransactionComputer{Balance: bal},
			balance.AccountCloser{Balance: bal},
		}
	)

	return &pipeline{
		Parser: parser.RecursiveParser{
			Context: ctx,
			File:    file,
		},
		Filter: ledger.Filter{
			Accounts:    accountsFilter,
			Commodities: commoditiesFilter,
		},
		Balances:        &result,
		ProcessingSteps: steps,
	}, nil
}

func (ppl *pipeline) process(w io.Writer) error {
	l, err := ppl.Parser.BuildLedger(ppl.Filter)
	if err != nil {
		return err
	}
	if l.Process(ppl.ProcessingSteps); err != nil {
		return err
	}
	var (
		j = balanceToJSON(*ppl.Balances)
		e = json.NewEncoder(w)
	)
	return e.Encode(j)
}

var periods = map[string]date.Period{
	"days":     date.Daily,
	"weeks":    date.Weekly,
	"months":   date.Monthly,
	"quarters": date.Quarterly,
	"years":    date.Yearly,
}

func parsePeriod(query url.Values, key string) (date.Period, error) {
	var (
		period date.Period
		value  string
		ok     bool
		err    error
	)
	if value, ok, err = getOne(query, key); err != nil {
		return date.Once, err
	}
	if !ok {
		return date.Once, nil
	}
	if period, ok = periods[value]; !ok {
		return date.Once, fmt.Errorf("invalid period %q", value)
	}
	return period, nil
}

func parseRegex(query url.Values, key string) (*regexp.Regexp, error) {
	var (
		s      string
		ok     bool
		err    error
		result *regexp.Regexp
	)
	if s, ok, err = getOne(query, key); err != nil {
		return nil, err
	}
	if ok {
		if result, err = regexp.Compile(s); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func parseDate(query url.Values, key string) (time.Time, error) {
	var (
		s   string
		ok  bool
		err error
		t   time.Time
	)
	if s, ok, err = getOne(query, key); err != nil || !ok {
		return t, err
	}

	return time.Parse("2006-01-02", s)
}

func parseInt(query url.Values, key string) (int, error) {
	var (
		s      string
		ok     bool
		err    error
		result int
	)
	if s, ok, err = getOne(query, key); err != nil {
		return result, err
	}
	if !ok {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func parseBool(query url.Values, key string) (bool, error) {
	var (
		s      string
		ok     bool
		err    error
		result bool
	)
	if s, ok, err = getOne(query, key); err != nil {
		return result, err
	}
	if !ok {
		return result, nil
	}
	return strconv.ParseBool(s)
}

func parseCommodity(query url.Values, ctx ledger.Context, key string) (*ledger.Commodity, error) {
	var (
		s   string
		ok  bool
		err error
	)
	if s, ok, err = getOne(query, key); err != nil || !ok {
		return nil, err
	}
	return ctx.GetCommodity(s)
}

func getOne(query url.Values, key string) (string, bool, error) {
	values, ok := query[key]
	if !ok {
		return "", ok, nil
	}
	if len(values) != 1 {
		return "", false, fmt.Errorf("expected one value for query parameter %q, got %v", key, values)
	}
	return values[0], true, nil
}

type jsonBalance struct {
	Valuation       *ledger.Commodity
	Dates           []time.Time
	Amounts, Values map[string]map[string][]decimal.Decimal
}

func balanceToJSON(bs []*balance.Balance) *jsonBalance {
	var res = jsonBalance{
		Valuation: bs[0].Valuation,
		Amounts:   make(map[string]map[string][]decimal.Decimal),
		Values:    make(map[string]map[string][]decimal.Decimal),
	}
	var wg sync.WaitGroup
	for i, b := range bs {
		res.Dates = append(res.Dates, b.Date)
		wg.Add(2)
		i := i
		b := b
		go func() {
			defer wg.Done()
			for pos, amount := range b.Amounts {
				insert(res.Amounts, i, len(bs), pos, amount)
			}
		}()
		go func() {
			defer wg.Done()
			for pos, value := range b.Amounts {
				insert(res.Values, i, len(bs), pos, value)
			}
		}()
		wg.Wait()
	}
	return &res
}

func insert(m map[string]map[string][]decimal.Decimal, i int, n int, pos balance.CommodityAccount, amount decimal.Decimal) {
	a, ok := m[pos.Account.String()]
	if !ok {
		a = make(map[string][]decimal.Decimal)
		m[pos.Account.String()] = a
	}
	c, ok := a[pos.Commodity.String()]
	if !ok {
		c = make([]decimal.Decimal, n)
		a[pos.Commodity.String()] = c
	}
	c[i] = amount
}
