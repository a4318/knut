package journal

import (
	"fmt"
	"strings"
	"time"

	"github.com/sboehler/knut/lib/common/compare"
	"github.com/sboehler/knut/lib/common/filter"
	"github.com/sboehler/knut/lib/common/mapper"
	"github.com/sboehler/knut/lib/common/set"
	"github.com/shopspring/decimal"
)

type DayFn = func(*Day) error

func NoOp[T any](_ T) error {
	return nil
}

// Error is a processing error, with a reference to a directive with
// a source location.
type Error struct {
	directive Directive
	msg       string
}

func (be Error) Error() string {
	var (
		p Printer
		b strings.Builder
	)
	fmt.Fprintf(&b, "%s:\n", be.directive.Position().Start)
	p.PrintDirective(&b, be.directive)
	fmt.Fprintf(&b, "\n%s\n", be.msg)
	return b.String()
}

// ComputePrices updates prices.
func ComputePrices(v *Commodity) DayFn {
	if v == nil {
		return NoOp[*Day]
	}
	var previous NormalizedPrices
	prc := make(Prices)
	return func(day *Day) error {
		if len(day.Prices) == 0 {
			day.Normalized = previous
		} else {
			for _, p := range day.Prices {
				prc.Insert(p.Commodity, p.Price, p.Target)
			}
			day.Normalized = prc.Normalize(v)
			previous = day.Normalized
		}
		return nil
	}
}

// Balance balances the journal.
func Balance(jctx Context, v *Commodity) DayFn {
	amounts, values := make(Amounts), make(Amounts)
	accounts := set.New[*Account]()

	processOpenings := func(d *Day) error {
		for _, o := range d.Openings {
			if accounts.Has(o.Account) {
				return Error{o, "account is already open"}
			}
			accounts.Add(o.Account)
		}
		return nil
	}

	processTransactions := func(d *Day) error {
		for _, t := range d.Transactions {
			for _, p := range t.Postings {
				if !accounts.Has(p.Account) {
					return Error{t, fmt.Sprintf("account %s is not open", p.Account)}
				}
				if p.Account.IsAL() {
					amounts.Add(AccountCommodityKey(p.Account, p.Commodity), p.Amount)
				}
			}
		}
		return nil
	}

	processValues := func(d *Day) error {
		for _, v := range d.Values {
			if !accounts.Has(v.Account) {
				return Error{v, "account is not open"}
			}
			valAcc := jctx.ValuationAccountFor(v.Account)
			amount := v.Amount.Sub(amounts.Amount(AccountCommodityKey(v.Account, v.Commodity)))
			ps := PostingBuilder{
				Credit:    valAcc,
				Debit:     v.Account,
				Commodity: v.Commodity,
				Amount:    amount,
				Targets:   []*Commodity{v.Commodity},
			}.Build()
			d.Transactions = append(d.Transactions, TransactionBuilder{
				Date:        v.Date,
				Description: fmt.Sprintf("Valuation adjustment for %s in %s", v.Commodity.Name(), v.Account.Name()),
				Postings:    ps,
			}.Build())
			amounts.Add(AccountCommodityKey(v.Account, v.Commodity), amount)
		}
		compare.Sort(d.Transactions, CompareTransactions)
		return nil
	}

	processAssertions := func(d *Day) error {
		for _, a := range d.Assertions {
			if !accounts.Has(a.Account) {
				return Error{a, "account is not open"}
			}
			position := AccountCommodityKey(a.Account, a.Commodity)
			if va, ok := amounts[position]; !ok || !va.Equal(a.Amount) {
				return Error{a, fmt.Sprintf("account has position: %s %s", va, position.Commodity.Name())}
			}
		}
		return nil
	}

	processClosings := func(d *Day) error {
		for _, c := range d.Closings {
			for pos, amount := range amounts {
				if pos.Account != c.Account {
					continue
				}
				if !amount.IsZero() {
					return Error{c, fmt.Sprintf("account has nonzero position: %s %s", amount, pos.Commodity.Name())}
				}
				delete(amounts, pos)
			}
			if !accounts.Has(c.Account) {
				return Error{c, "account is not open"}
			}
			accounts.Remove(c.Account)
		}
		return nil
	}

	valuateTransactions := func(d *Day) error {
		for _, t := range d.Transactions {
			for _, posting := range t.Postings {
				if v != posting.Commodity {
					v, err := d.Normalized.Valuate(posting.Commodity, posting.Amount)
					if err != nil {
						return err
					}
					posting.Value = v
				} else {
					posting.Value = posting.Amount
				}
				if posting.Account.IsAL() {
					values.Add(AccountCommodityKey(posting.Account, posting.Commodity), posting.Value)
				}
			}
		}
		return nil
	}

	valuateGains := func(d *Day) error {
		for pos, amt := range amounts {
			if pos.Commodity == v {
				continue
			}
			if !pos.Account.IsAL() {
				continue
			}
			value, err := d.Normalized.Valuate(pos.Commodity, amt)
			if err != nil {
				return fmt.Errorf("no valuation found for commodity %s", pos.Commodity.Name())
			}
			gain := value.Sub(values[pos])
			if gain.IsZero() {
				continue
			}
			credit := jctx.ValuationAccountFor(pos.Account)
			d.Transactions = append(d.Transactions, TransactionBuilder{
				Date:        d.Date,
				Description: fmt.Sprintf("Adjust value of %s in account %s", pos.Commodity.Name(), pos.Account.Name()),
				Postings: PostingBuilder{
					Credit:    credit,
					Debit:     pos.Account,
					Commodity: pos.Commodity,
					Value:     gain,
					Targets:   []*Commodity{pos.Commodity},
				}.Build(),
			}.Build())
			values.Add(pos, gain)
			values.Add(AccountCommodityKey(credit, pos.Commodity), gain.Neg())
		}
		return nil

	}

	return func(d *Day) error {
		if err := processOpenings(d); err != nil {
			return err
		}
		if err := processTransactions(d); err != nil {
			return err
		}
		if err := processValues(d); err != nil {
			return err
		}
		if err := processAssertions(d); err != nil {
			return err
		}
		if v != nil {
			if err := valuateTransactions(d); err != nil {
				return err
			}
			if err := valuateGains(d); err != nil {
				return err
			}
		}
		if err := processClosings(d); err != nil {
			return err
		}
		return nil
	}
}

// Balance balances the journal.
func CloseAccounts(j *Journal, ds []time.Time) DayFn {
	var (
		closingDays     = set.New[*Day]()
		amounts, values = make(Amounts), make(Amounts)
	)
	for _, d := range ds {
		closingDays.Add(j.Day(d.AddDate(0, 0, 1)))
	}
	return func(d *Day) error {
		if closingDays.Has(d) {
			for k, amt := range amounts {
				if !k.Account.IsIE() {
					continue
				}
				d.Transactions = append(d.Transactions, TransactionBuilder{
					Date:        d.Date,
					Description: fmt.Sprintf("Closing account %s in %s", k.Account.Name(), k.Commodity.Name()),
					Postings: PostingBuilder{
						Credit:    k.Account,
						Debit:     j.Context.Account("Equity:Equity"),
						Commodity: k.Commodity,
						Amount:    amt,
						Value:     values[k],
					}.Build(),
				}.Build())
			}
		}
		for _, t := range d.Transactions {
			for _, p := range t.Postings {
				if p.Account.IsIE() {
					amounts.Add(AccountCommodityKey(p.Account, p.Commodity), p.Amount)
					values.Add(AccountCommodityKey(p.Account, p.Commodity), p.Value)
				}
			}
		}
		return nil
	}
}

// Sort sorts the directives in this day.
func Sort() DayFn {
	return func(d *Day) error {
		compare.Sort(d.Transactions, CompareTransactions)
		return nil
	}
}

type Collection interface {
	Insert(k Key, v decimal.Decimal)
}

func Query(f filter.Filter[Key], m mapper.Mapper[Key], v *Commodity, c Collection) DayFn {
	if f == nil {
		f = filter.AllowAll[Key]
	}
	if m == nil {
		m = mapper.Identity[Key]
	}
	return func(d *Day) error {
		for _, t := range d.Transactions {
			for _, b := range t.Postings {
				amt := b.Amount
				if v != nil {
					amt = b.Value
				}
				kc := Key{
					Date:        t.Date,
					Account:     b.Account,
					Other:       b.Other,
					Commodity:   b.Commodity,
					Valuation:   v,
					Description: t.Description,
				}
				if f(kc) {
					c.Insert(m(kc), amt)
				}
			}
		}
		return nil
	}
}
