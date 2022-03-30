package process

import (
	"context"
	"fmt"

	"github.com/sboehler/knut/lib/common/amounts"
	"github.com/sboehler/knut/lib/common/cpr"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/ast"
	"golang.org/x/sync/errgroup"
)

// Balancer processes ASTs.
type Balancer struct {
	Context journal.Context
}

// Process2 processes days.
func (pr *Balancer) Process2(ctx context.Context, g *errgroup.Group, inCh <-chan *ast.Day) <-chan *ast.Day {

	resCh := make(chan *ast.Day, 100)

	g.Go(func() error {
		defer close(resCh)

		amounts := make(amounts.Amounts)
		accounts := make(accounts)

		for {
			d, ok, err := cpr.Pop(ctx, inCh)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			var transactions []*ast.Transaction
			if err := pr.processOpenings(ctx, accounts, d); err != nil {
				return err
			}
			if err := pr.processTransactions(ctx, accounts, amounts, d); err != nil {
				return err
			}
			if transactions, err = pr.processValues(ctx, accounts, amounts, d); err != nil {
				return err
			}
			if err = pr.processAssertions(ctx, accounts, amounts, d); err != nil {
				return err
			}
			if err = pr.processClosings(ctx, accounts, amounts, d); err != nil {
				return err
			}

			d.Transactions = append(d.Transactions, transactions...)
			d.Amounts = amounts.Clone()

			if err := cpr.Push(ctx, resCh, d); err != nil {
				return err
			}
		}
		return nil
	})
	return resCh
}

func (pr *Balancer) processOpenings(ctx context.Context, accounts accounts, d *ast.Day) error {
	for _, o := range d.Openings {
		if err := accounts.Open(o.Account); err != nil {
			return err
		}
	}
	return nil
}

func (pr *Balancer) processTransactions(ctx context.Context, accounts accounts, amounts amounts.Amounts, d *ast.Day) error {
	for _, t := range d.Transactions {
		for _, p := range t.Postings {
			if !accounts.IsOpen(p.Credit) {
				return Error{t, fmt.Sprintf("credit account %s is not open", p.Credit)}
			}
			if !accounts.IsOpen(p.Debit) {
				return Error{t, fmt.Sprintf("debit account %s is not open", p.Debit)}
			}
			amounts.Book(p.Credit, p.Debit, p.Amount, p.Commodity)
		}
	}
	return nil
}

func (pr *Balancer) processValues(ctx context.Context, accounts accounts, amounts amounts.Amounts, d *ast.Day) ([]*ast.Transaction, error) {
	var transactions []*ast.Transaction
	for _, v := range d.Values {
		if !accounts.IsOpen(v.Account) {
			return nil, Error{v, "account is not open"}
		}
		valAcc := pr.Context.ValuationAccountFor(v.Account)
		posting := ast.NewPostingWithTargets(valAcc, v.Account, v.Commodity, v.Amount.Sub(amounts.Amount(v.Account, v.Commodity)), []*journal.Commodity{v.Commodity})
		amounts.Book(posting.Credit, posting.Debit, posting.Amount, posting.Commodity)
		transactions = append(transactions, &ast.Transaction{
			Date:        v.Date,
			Description: fmt.Sprintf("Valuation adjustment for %v in %v", v.Commodity, v.Account),
			Tags:        nil,
			Postings:    []ast.Posting{posting},
		})
	}
	return transactions, nil
}

func (pr *Balancer) processAssertions(ctx context.Context, accounts accounts, amts amounts.Amounts, d *ast.Day) error {
	for _, a := range d.Assertions {
		if !accounts.IsOpen(a.Account) {
			return Error{a, "account is not open"}
		}
		position := amounts.CommodityAccount{Account: a.Account, Commodity: a.Commodity}
		if va, ok := amts[position]; !ok || !va.Equal(a.Amount) {
			return Error{a, fmt.Sprintf("assertion failed: account %s has %s %s", a.Account, va, position.Commodity)}
		}
	}
	return nil
}

func (pr *Balancer) processClosings(ctx context.Context, accounts accounts, amounts amounts.Amounts, d *ast.Day) error {
	for _, c := range d.Closings {
		for pos, amount := range amounts {
			if pos.Account != c.Account {
				continue
			}
			if !amount.IsZero() {
				return Error{c, "account has nonzero position"}
			}
			delete(amounts, pos)
		}
		if err := accounts.Close(c.Account); err != nil {
			return err
		}
	}
	return nil
}

// accounts keeps track of open accounts.
type accounts map[*journal.Account]bool

// Open opens an account.
func (oa accounts) Open(a *journal.Account) error {
	if oa[a] {
		return fmt.Errorf("account %v is already open", a)
	}
	oa[a] = true
	return nil
}

// Close closes an account.
func (oa accounts) Close(a *journal.Account) error {
	if !oa[a] {
		return fmt.Errorf("account %v is already closed", a)
	}
	delete(oa, a)
	return nil
}

// IsOpen returns whether an account is open.
func (oa accounts) IsOpen(a *journal.Account) bool {
	return oa[a] || a.Type() == journal.EQUITY
}