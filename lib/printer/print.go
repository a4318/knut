package printer

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/sboehler/knut/lib/ledger"
	"github.com/sboehler/knut/lib/model/accounts"
)

// Printer prints directives.
type Printer struct {
	Padding int
}

// New creates a new Printer.
func New() *Printer {
	return new(Printer)
}

// PrintDirective prints a directive to the given Writer.
func (p Printer) PrintDirective(w io.Writer, directive interface{}) (n int, err error) {
	switch d := directive.(type) {
	case ledger.Transaction:
		return p.printTransaction(w, d)
	case ledger.Open:
		return p.printOpen(w, d)
	case ledger.Close:
		return p.printClose(w, d)
	case ledger.Assertion:
		return p.printAssertion(w, d)
	case ledger.Include:
		return p.printInclude(w, d)
	case ledger.Price:
		return p.printPrice(w, d)
	case ledger.Accrual:
		return p.printAccrual(w, d)
	case ledger.Value:
		return p.printValue(w, d)
	}
	return 0, fmt.Errorf("unknown directive: %v", directive)
}

func (p Printer) printTransaction(w io.Writer, t ledger.Transaction) (n int, err error) {
	c, err := fmt.Fprintf(w, "%s \"%s\"", t.Date.Format("2006-01-02"), t.Description)
	n += c
	if err != nil {
		return n, err
	}
	for _, tag := range t.Tags {
		c, err := fmt.Fprintf(w, " %s", tag)
		n += c
		if err != nil {
			return n, err
		}
	}
	c, err = io.WriteString(w, "\n")
	n += c
	if err != nil {
		return n, err
	}
	for _, po := range t.Postings {
		d, err := p.printPosting(w, po)
		n += d
		if err != nil {
			return n, err
		}
		c, err = io.WriteString(w, "\n")
		n += c
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (p Printer) printAccrual(w io.Writer, a ledger.Accrual) (n int, err error) {
	c, err := fmt.Fprintf(w, "@accrue %s %s %s %s\n", a.Period, a.T0.Format("2006-01-02"), a.T1.Format("2006-01-02"), a.Account)
	n += c
	if err != nil {
		return n, err
	}
	c, err = p.printTransaction(w, a.Transaction)
	return n + c, err
}

func (p Printer) printPosting(w io.Writer, t ledger.Posting) (int, error) {
	var n int
	c, err := fmt.Fprintf(w, "%s %s %s %s", p.rightPad(t.Credit), p.rightPad(t.Debit), leftPad(10, t.Amount.String()), t.Commodity)
	n += c
	if err != nil {
		return n, err
	}
	if t.Lot != nil {
		c, err = io.WriteString(w, " ")
		n += c
		if err != nil {
			return n, err
		}
		d, err := p.printLot(w, t.Lot)
		n += d
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (p Printer) printLot(w io.Writer, l *ledger.Lot) (int, error) {
	var n int
	c, err := fmt.Fprintf(w, "{ %g %s, %s ", l.Price, l.Commodity, l.Date.Format("2006-01-02"))
	n += c
	if err != nil {
		return n, err
	}
	if len(l.Label) > 0 {
		c, err = fmt.Fprintf(w, "%s ", l.Label)
		n += c
		if err != nil {
			return n, err
		}
	}
	c, err = io.WriteString(w, "}")
	n += c
	if err != nil {
		return n, err
	}
	return n, nil
}

func (p Printer) printOpen(w io.Writer, o ledger.Open) (int, error) {
	return fmt.Fprintf(w, "%s open %s", o.Date.Format("2006-01-02"), o.Account)
}

func (p Printer) printClose(w io.Writer, c ledger.Close) (int, error) {
	return fmt.Fprintf(w, "%s close %s", c.Date.Format("2006-01-02"), c.Account)
}

func (p Printer) printPrice(w io.Writer, pr ledger.Price) (int, error) {
	return fmt.Fprintf(w, "%s price %s %s %s", pr.Date.Format("2006-01-02"), pr.Commodity, pr.Price, pr.Target)
}

func (p Printer) printInclude(w io.Writer, i ledger.Include) (int, error) {
	return fmt.Fprintf(w, "include \"%s\"", i.Path)
}

func (p Printer) printAssertion(w io.Writer, a ledger.Assertion) (int, error) {
	return fmt.Fprintf(w, "%s balance %s %s %s", a.Date.Format("2006-01-02"), a.Account, a.Amount, a.Commodity)
}

func (p Printer) printValue(w io.Writer, v ledger.Value) (int, error) {
	return fmt.Fprintf(w, "%s value %s %s %s", v.Date.Format("2006-01-02"), v.Account, v.Amount, v.Commodity)
}

// PrintLedger prints a Ledger.
func (p *Printer) PrintLedger(w io.Writer, l ledger.Ledger) (int, error) {
	for _, day := range l.Days {
		for _, t := range day.Transactions {
			p.updatePadding(t)
		}
	}
	var n int
	for _, day := range l.Days {
		for _, pr := range day.Prices {
			if err := p.writeLn(w, pr, &n); err != nil {
				return n, err
			}
		}
		for _, o := range day.Openings {
			if err := p.writeLn(w, o, &n); err != nil {
				return n, err
			}
		}
		for _, t := range day.Transactions {
			if err := p.writeLn(w, t, &n); err != nil {
				return n, err
			}
		}
		for _, v := range day.Values {
			if err := p.writeLn(w, v, &n); err != nil {
				return n, err
			}
		}
		for _, a := range day.Assertions {
			if err := p.writeLn(w, a, &n); err != nil {
				return n, err
			}
		}
		for _, c := range day.Closings {
			if err := p.writeLn(w, c, &n); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// Initialize initializes the padding of this printer.
func (p *Printer) Initialize(directive []ledger.Directive) {
	for _, d := range directive {
		switch t := d.(type) {
		case ledger.Transaction:
			p.updatePadding(t)
		}
	}
}

func (p *Printer) updatePadding(t ledger.Transaction) {
	for _, pt := range t.Postings {
		var cr, dr = utf8.RuneCountInString(pt.Credit.String()), utf8.RuneCountInString(pt.Debit.String())
		if p.Padding < cr {
			p.Padding = cr
		}
		if p.Padding < dr {
			p.Padding = dr
		}
	}
}

func (p Printer) writeLn(w io.Writer, d interface{}, count *int) error {
	c, err := p.PrintDirective(w, d)
	*count += c
	if err != nil {
		return err
	}
	c, err = io.WriteString(w, "\n")
	*count += c
	if err != nil {
		return err
	}
	return nil
}

func (p Printer) rightPad(a *accounts.Account) string {
	var b strings.Builder
	b.WriteString(a.String())
	for i := utf8.RuneCountInString(a.String()); i < p.Padding; i++ {
		b.WriteRune(' ')
	}
	return b.String()
}

func leftPad(n int, s string) string {
	if len(s) > n {
		return s
	}
	var b strings.Builder
	for i := 0; i < n-len(s); i++ {
		b.WriteRune(' ')
	}
	b.WriteString(s)
	return b.String()
}
