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

package journal

import (
	"time"

	"github.com/sboehler/knut/lib/common/compare"
	"github.com/sboehler/knut/lib/common/date"
	"github.com/sboehler/knut/lib/common/dict"
)

// Journal represents an unprocessed
type Journal struct {
	Context  Context
	Days     map[time.Time]*Day
	min, max time.Time
}

// New creates a new AST
func New(ctx Context) *Journal {
	return &Journal{
		Context: ctx,
		Days:    make(map[time.Time]*Day),
		min:     date.Date(9999, 12, 31),
		max:     time.Time{},
	}
}

// Day returns the Day for the given date.
func (ast *Journal) Day(d time.Time) *Day {
	return dict.GetDefault(ast.Days, d, func() *Day { return &Day{Date: d} })
}

// SortedDays returns all days ordered by date.
func (ast *Journal) SortedDays() []*Day {
	var res []*Day
	for _, day := range ast.Days {
		compare.Sort(day.Transactions, CompareTransactions)
		res = append(res, day)
	}
	compare.Sort(res, CompareDays)
	return res
}

// AddOpen adds an Open directive.
func (ast *Journal) AddOpen(o *Open) {
	d := ast.Day(o.Date)
	d.Openings = append(d.Openings, o)
}

// AddPrice adds an Price directive.
func (ast *Journal) AddPrice(p *Price) {
	d := ast.Day(p.Date)
	d.Prices = append(d.Prices, p)
}

// AddTransaction adds an Transaction directive.
func (ast *Journal) AddTransaction(t *Transaction) {
	d := ast.Day(t.Date)
	if ast.max.Before(d.Date) {
		ast.max = d.Date
	}
	if ast.min.After(t.Date) {
		ast.min = d.Date
	}
	d.Transactions = append(d.Transactions, t)
}

// AddValue adds an Value directive.
func (ast *Journal) AddValue(v *Value) {
	d := ast.Day(v.Date)
	d.Values = append(d.Values, v)
}

// AddAssertion adds an Assertion directive.
func (ast *Journal) AddAssertion(a *Assertion) {
	d := ast.Day(a.Date)
	d.Assertions = append(d.Assertions, a)
}

// AddClose adds an Close directive.
func (ast *Journal) AddClose(c *Close) {
	d := ast.Day(c.Date)
	d.Closings = append(d.Closings, c)
}

func (ast *Journal) Min() time.Time {
	return ast.min
}

func (ast *Journal) Max() time.Time {
	return ast.max
}

// Day groups all commands for a given date.
type Day struct {
	Date         time.Time
	Prices       []*Price
	Assertions   []*Assertion
	Values       []*Value
	Openings     []*Open
	Transactions []*Transaction
	Closings     []*Close

	Amounts, Value Amounts

	Normalized NormalizedPrices

	Performance *Performance
}

// Less establishes an ordering on Day.
func CompareDays(d *Day, d2 *Day) compare.Order {
	return compare.Time(d.Date, d2.Date)
}

// Performance holds aggregate information used to compute
// portfolio performance.
type Performance struct {
	V0, V1, Inflow, Outflow, InternalInflow, InternalOutflow map[*Commodity]float64
	PortfolioInflow, PortfolioOutflow                        float64
}
