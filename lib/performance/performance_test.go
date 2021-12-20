package performance

import (
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/ast"
	"github.com/sboehler/knut/lib/journal/past"
	"github.com/shopspring/decimal"
)

func TestFlowComputer(t *testing.T) {
	var (
		ctx          = journal.NewContext()
		chf, _       = ctx.GetCommodity("CHF")
		usd, _       = ctx.GetCommodity("USD")
		gbp, _       = ctx.GetCommodity("GBP")
		aapl, _      = ctx.GetCommodity("AAPL")
		portfolio, _ = ctx.GetAccount("Assets:Portfolio")
		acc1, _      = ctx.GetAccount("Assets:Acc1")
		acc2, _      = ctx.GetAccount("Assets:Acc2")
		dividend, _  = ctx.GetAccount("Income:Dividends")
		expense, _   = ctx.GetAccount("Expenses:Investments")
		equity, _    = ctx.GetAccount("Equity:Equity")
	)
	chf.IsCurrency = true
	usd.IsCurrency = true
	gbp.IsCurrency = true

	var (
		tests = []struct {
			desc string
			trx  *ast.Transaction
			want *DailyPerfValues
		}{
			{
				desc: "outflow",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: acc2, Amount: decimal.NewFromInt(2), Value: decimal.NewFromInt(1), Commodity: usd},
					},
				},
				want: &DailyPerfValues{Outflow: pcv{usd: -1.0}},
			},
			{
				desc: "inflow",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: acc1, Debit: portfolio, Amount: decimal.NewFromInt(2), Value: decimal.NewFromInt(1), Commodity: usd},
					},
				},
				want: &DailyPerfValues{Inflow: pcv{usd: 1.0}},
			},
			{
				desc: "dividend",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: dividend, Debit: portfolio, Amount: decimal.NewFromInt(2), Value: decimal.NewFromInt(1), Commodity: usd, TargetCommodity: aapl},
					},
				},
				want: &DailyPerfValues{
					InternalInflow:  pcv{usd: 1.0},
					InternalOutflow: pcv{aapl: -1.0},
				},
			},
			{
				desc: "expense",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: expense, Amount: decimal.NewFromInt(2), Value: decimal.NewFromInt(1), Commodity: usd, TargetCommodity: aapl},
					},
				},
				want: &DailyPerfValues{
					InternalInflow:  pcv{aapl: 1.0},
					InternalOutflow: pcv{usd: -1.0},
				},
			},
			{
				desc: "stock purchase",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(1100), Value: decimal.NewFromInt(1010), Commodity: usd},
						{Credit: equity, Debit: portfolio, Amount: decimal.NewFromInt(1), Value: decimal.NewFromInt(1000), Commodity: aapl},
					},
				},
				want: &DailyPerfValues{
					InternalInflow:  pcv{aapl: 1010.0},
					InternalOutflow: pcv{usd: -1010.0},
				},
			},
			{
				desc: "stock purchase with fee",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(1100), Value: decimal.NewFromInt(1010), Commodity: usd},
						{Credit: equity, Debit: portfolio, Amount: decimal.NewFromInt(1), Value: decimal.NewFromInt(1000), Commodity: aapl},
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(10), Value: decimal.NewFromInt(10), Commodity: usd},
					},
				},
				want: &DailyPerfValues{
					InternalInflow:  pcv{aapl: 1020.0},
					InternalOutflow: pcv{usd: -1020.0},
				},
			},
			{
				desc: "stock sale",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(1), Value: decimal.NewFromInt(1000), Commodity: aapl},
						{Credit: equity, Debit: portfolio, Amount: decimal.NewFromInt(1100), Value: decimal.NewFromInt(990), Commodity: usd},
					},
				},
				want: &DailyPerfValues{
					InternalInflow:  pcv{usd: 990.0},
					InternalOutflow: pcv{aapl: -990.0},
				},
			},

			{
				desc: "forex without fee",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(1000), Value: decimal.NewFromInt(1400), Commodity: gbp},
						{Credit: equity, Debit: portfolio, Amount: decimal.NewFromInt(1500), Value: decimal.NewFromInt(1350), Commodity: usd},
					},
				},
				want: &DailyPerfValues{
					InternalOutflow: pcv{gbp: -1375.0},
					InternalInflow:  pcv{usd: 1375.0},
				},
			},
			{
				desc: "forex with fee",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(1000), Value: decimal.NewFromInt(1400), Commodity: gbp},
						{Credit: equity, Debit: portfolio, Amount: decimal.NewFromInt(1500), Value: decimal.NewFromInt(1350), Commodity: usd},
						{Credit: portfolio, Debit: expense, Amount: decimal.NewFromInt(10), Value: decimal.NewFromInt(10), Commodity: chf},
					},
				},
				want: &DailyPerfValues{
					InternalOutflow: pcv{gbp: -1370.0, chf: -10},
					InternalInflow:  pcv{usd: 1380.0},
				},
			},
			{
				desc: "forex with native fee",
				trx: &ast.Transaction{
					Postings: []ast.Posting{
						{Credit: portfolio, Debit: equity, Amount: decimal.NewFromInt(1000), Value: decimal.NewFromInt(1400), Commodity: gbp},
						{Credit: equity, Debit: portfolio, Amount: decimal.NewFromInt(1500), Value: decimal.NewFromInt(1350), Commodity: usd},
						{Credit: portfolio, Debit: expense, Amount: decimal.NewFromInt(10), Value: decimal.NewFromInt(10), Commodity: usd},
					},
				},
				want: &DailyPerfValues{
					InternalOutflow: pcv{gbp: -1370.0},
					InternalInflow:  pcv{usd: 1370.0},
				},
			},
		}
	)
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			var (
				d = &past.Day{
					Date:         time.Date(2021, 11, 15, 0, 0, 0, 0, time.UTC),
					Transactions: []*ast.Transaction{test.trx},
				}

				fc = FlowComputer{
					Result:    new(DailyPerfValues),
					Filter:    journal.Filter{Accounts: regexp.MustCompile("Assets:Portfolio")},
					Valuation: chf,
				}
			)
			if err := fc.Process(d); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(test.want, fc.Result); diff != "" {
				t.Fatalf("unexpected diff (-want, +got):\n%s", diff)
			}
		})
	}

}
