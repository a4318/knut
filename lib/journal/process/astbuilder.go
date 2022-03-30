package process

import (
	"context"
	"fmt"
	"time"

	"github.com/sboehler/knut/lib/common/cpr"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/ast"
	"github.com/sboehler/knut/lib/journal/ast/parser"
	"golang.org/x/sync/errgroup"
)

// ASTBuilder builds an abstract syntax tree.
type ASTBuilder struct {
	Context journal.Context

	Journal string
	Expand  bool
	Filter  journal.Filter

	ast *ast.AST
}

// Source2 is a source of days.
func (pr *ASTBuilder) Source2(ctx context.Context, g *errgroup.Group) <-chan *ast.Day {
	pr.ast = &ast.AST{
		Context: pr.Context,
		Days:    make(map[time.Time]*ast.Day),
	}
	p := parser.RecursiveParser{
		Context: pr.Context,
		File:    pr.Journal,
	}
	resCh := make(chan *ast.Day)

	g.Go(func() error {
		defer close(resCh)

		ch, errCh := p.Parse(ctx)
		for {
			d, ok, err := cpr.Get(ch, errCh)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			switch t := d.(type) {

			case *ast.Open:
				pr.ast.AddOpen(t)

			case *ast.Price:
				pr.ast.AddPrice(t)

			case *ast.Transaction:
				var filtered []ast.Posting
				for _, p := range t.Postings {
					if p.Matches(pr.Filter) {
						filtered = append(filtered, p)
					}
				}
				if len(filtered) == 0 {
					break
				}
				if len(filtered) < len(t.Postings) {
					t.Postings = filtered
				}
				if len(t.AddOns) > 0 {
					for _, addOn := range t.AddOns {
						switch acc := addOn.(type) {
						case *ast.Accrual:
							for _, ts := range acc.Expand(t) {
								pr.ast.AddTransaction(ts)
							}
						default:
							panic(fmt.Sprintf("unknown addon: %#v", acc))
						}
					}
				} else {
					pr.ast.AddTransaction(t)
				}

			case *ast.Assertion:
				if !pr.Filter.MatchAccount(t.Account) {
					break
				}
				if !pr.Filter.MatchCommodity(t.Commodity) {
					break
				}
				pr.ast.AddAssertion(t)

			case *ast.Value:
				if !pr.Filter.MatchAccount(t.Account) {
					break
				}
				if !pr.Filter.MatchCommodity(t.Commodity) {
					break
				}
				pr.ast.AddValue(t)

			case *ast.Close:
				if !pr.Filter.MatchAccount(t.Account) {
					break
				}
				pr.ast.AddClose(t)

			default:
				return fmt.Errorf("unknown: %#v", t)
			}
		}
		for _, d := range pr.ast.SortedDays() {
			if err := cpr.Push(ctx, resCh, d); err != nil {
				return err
			}
		}
		return nil
	})
	return resCh
}
