package process

import (
	"context"
	"fmt"

	"github.com/sboehler/knut/lib/common/amounts"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/ast"
	"github.com/sboehler/knut/lib/journal/val"
)

// Valuator produces valuated days.
type Valuator struct {
	Context   journal.Context
	Valuation *journal.Commodity

	//values amounts.Amounts
}

// ProcessStream computes prices.
func (pr *Valuator) ProcessStream(ctx context.Context, inCh <-chan *val.Day) (chan *val.Day, chan error) {
	errCh := make(chan error)
	resCh := make(chan *val.Day, 100)

	values := make(amounts.Amounts)

	go func() {
		defer close(resCh)
		defer close(errCh)

		for {
			day, ok, err := pop(ctx, inCh)
			if !ok || err != nil {
				return
			}
			day.Values = values

			for _, t := range day.Day.Transactions {
				if errors := pr.valuateAndBookTransaction(ctx, day, t); push(ctx, errCh, errors...) != nil {
					return
				}
			}

			pr.computeValuationTransactions(day)
			values = values.Clone()

			if push(ctx, resCh, day) != nil {
				return
			}
		}
	}()

	return resCh, errCh
}

func (pr Valuator) valuateAndBookTransaction(ctx context.Context, day *val.Day, t *ast.Transaction) []error {
	var errors []error
	tx := t.Clone()
	for i, posting := range t.Postings {
		if pr.Valuation != nil && pr.Valuation != posting.Commodity {
			var err error
			if posting.Amount, err = day.Prices.Valuate(posting.Commodity, posting.Amount); err != nil {
				errors = append(errors, err)
			}
		}
		day.Values.Book(posting.Credit, posting.Debit, posting.Amount, posting.Commodity)
		tx.Postings[i] = posting
	}
	day.Transactions = append(day.Transactions, tx)
	return errors
}

// computeValuationTransactions checks whether the valuation for the positions
// corresponds to the amounts. If not, the difference is due to a valuation
// change of the previous amount, and a transaction is created to adjust the
// valuation.
func (pr Valuator) computeValuationTransactions(day *val.Day) {
	if pr.Valuation == nil {
		return
	}
	for pos, va := range day.Day.Amounts {
		if pos.Commodity == pr.Valuation {
			continue
		}
		var at = pos.Account.Type()
		if at != journal.ASSETS && at != journal.LIABILITIES {
			continue
		}
		value, err := day.Prices.Valuate(pos.Commodity, va)
		if err != nil {
			panic(fmt.Sprintf("no valuation found for commodity %s", pos.Commodity))
		}
		var diff = value.Sub(day.Values[pos])
		if diff.IsZero() {
			continue
		}
		valAcc, err := pr.Context.ValuationAccountFor(pos.Account)
		if err != nil {
			panic(fmt.Sprintf("could not obtain valuation account for account %s", pos.Account))
		}
		desc := fmt.Sprintf("Adjust value of %v in account %v", pos.Commodity, pos.Account)
		if !diff.IsZero() {
			day.Transactions = append(day.Transactions, &ast.Transaction{
				Date:        day.Date,
				Description: desc,
				Postings: []ast.Posting{
					ast.NewPosting(valAcc, pos.Account, pos.Commodity, diff),
				},
			})
			day.Values.Book(valAcc, pos.Account, diff, pos.Commodity)
		}
	}
}