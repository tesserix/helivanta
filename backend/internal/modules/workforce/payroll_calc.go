package workforce

import (
	"errors"

	"github.com/google/uuid"
)

// Calculation limits: any line above maxPayrollLinePaise fails closed, so percentage chains can never overflow int64.
const (
	maxPayrollAmountPaise int64 = 1_000_000_000
	maxPayrollLinePaise   int64 = 100_000_000_000
	payrollRateScale      int64 = 1_000_000
)

var (
	errPayrollCycle      = errors.New("salary heads form a dependency cycle")
	errPayrollIncomplete = errors.New("salary structure cannot be computed")
	errPayrollOverflow   = errors.New("salary line exceeds the calculation limit")
)

// payrollHead is one head revision as the calculator sees it; amounts are paise, rates parts per million.
type payrollHead struct {
	HeadID, RevisionID uuid.UUID
	Code, Kind, Basis  string
	RatePPM            int64
	Bases              []uuid.UUID
	Proration          string
	ProrationDays      int
	MinPayableHalfDays int
	Months             []int
	RoundingMode       string
	RoundingUnit       int64
	MinAmount          *int64
	MaxAmount          *int64
	InGross, InCTC     bool
	WageClass          string
	Active             bool
}

type payrollLineInput struct {
	HeadID uuid.UUID
	Amount *int64
}

type payrollCalcInput struct {
	Heads           map[uuid.UUID]payrollHead
	Lines           []payrollLineInput
	Month           int
	PeriodDays      int
	PayableHalfDays int
}

type payrollCalcLine struct {
	HeadID     uuid.UUID
	RevisionID uuid.UUID
	Amount     int64
	Applicable bool
}

type payrollTotals struct {
	Gross, Deductions, Employer, Net, CTC int64
	NegativeNet                           bool
}

type payrollCodeWages struct {
	Wages, Excluded, Remuneration, AddBack, StatutoryWages int64
	ExcludedSharePPM                                       int64
}

type payrollCalcResult struct {
	Lines     []payrollCalcLine
	Totals    payrollTotals
	CodeWages payrollCodeWages
}

// roundPaise rounds num/den to a multiple of unit; num and den are non-negative and den positive.
func roundPaise(num, den, unit int64, mode string) int64 {
	step := den * unit
	q, r := num/step, num%step
	switch mode {
	case "up":
		if r > 0 {
			q++
		}
	case "nearest":
		if 2*r >= step {
			q++
		}
	}
	return q * unit
}

func monthApplies(months []int, month int) bool {
	for _, m := range months {
		if m == month {
			return true
		}
	}
	return false
}

// calculatePayroll evaluates a structure in dependency order: applicability, then proration or percentage, then rounding, then floor and cap.
func calculatePayroll(in payrollCalcInput) (payrollCalcResult, error) {
	amounts := make(map[uuid.UUID]int64, len(in.Lines))
	lines := make(map[uuid.UUID]payrollLineInput, len(in.Lines))
	for _, l := range in.Lines {
		h, ok := in.Heads[l.HeadID]
		if !ok || !h.Active || (h.Basis == "fixed") != (l.Amount != nil) {
			return payrollCalcResult{}, errPayrollIncomplete
		}
		lines[l.HeadID] = l
	}
	state := make(map[uuid.UUID]int8, len(in.Lines))
	applicable := make(map[uuid.UUID]bool, len(in.Lines))
	var visit func(id uuid.UUID) error
	visit = func(id uuid.UUID) error {
		switch state[id] {
		case 1:
			return errPayrollCycle
		case 2:
			return nil
		}
		state[id] = 1
		h := in.Heads[id]
		var base int64
		for _, b := range h.Bases {
			bh, ok := in.Heads[b]
			if _, inStructure := lines[b]; !ok || !inStructure || bh.Kind != "earning" {
				return errPayrollIncomplete
			}
			if err := visit(b); err != nil {
				return err
			}
			base += amounts[b]
		}
		state[id] = 2
		if !monthApplies(h.Months, in.Month) || in.PayableHalfDays < h.MinPayableHalfDays {
			amounts[id] = 0
			return nil
		}
		applicable[id] = true
		var num, den int64
		if h.Basis == "fixed" {
			num, den = *lines[id].Amount, 1
			switch h.Proration {
			case "calendar_days":
				num, den = num*int64(in.PayableHalfDays), 2*int64(in.PeriodDays)
			case "fixed_days":
				if in.PayableHalfDays < 2*h.ProrationDays {
					num, den = num*int64(in.PayableHalfDays), 2*int64(h.ProrationDays)
				}
			}
		} else {
			num, den = base*h.RatePPM, payrollRateScale
		}
		amount := roundPaise(num, den, h.RoundingUnit, h.RoundingMode)
		if h.MinAmount != nil && amount < *h.MinAmount {
			amount = *h.MinAmount
		}
		if h.MaxAmount != nil && amount > *h.MaxAmount {
			amount = *h.MaxAmount
		}
		if amount > maxPayrollLinePaise {
			return errPayrollOverflow
		}
		amounts[id] = amount
		return nil
	}
	result := payrollCalcResult{Lines: make([]payrollCalcLine, 0, len(in.Lines))}
	for _, l := range in.Lines {
		if err := visit(l.HeadID); err != nil {
			return payrollCalcResult{}, err
		}
	}
	t, w := &result.Totals, &result.CodeWages
	for _, l := range in.Lines {
		h, amount := in.Heads[l.HeadID], amounts[l.HeadID]
		result.Lines = append(result.Lines, payrollCalcLine{HeadID: l.HeadID, RevisionID: h.RevisionID, Amount: amount, Applicable: applicable[l.HeadID]})
		switch h.Kind {
		case "earning":
			if h.InGross {
				t.Gross += amount
			}
		case "deduction":
			t.Deductions += amount
		case "employer_contribution":
			t.Employer += amount
		}
		if h.InCTC {
			t.CTC += amount
		}
		// Deductions are never remuneration, whatever their declared class.
		switch {
		case h.Kind == "deduction":
		case h.WageClass == "wages":
			w.Wages += amount
		case h.WageClass == "excluded":
			w.Excluded += amount
		}
	}
	t.Net = t.Gross - t.Deductions
	t.NegativeNet = t.Net < 0
	w.Remuneration = w.Wages + w.Excluded
	if over := w.Excluded - w.Remuneration/2; over > 0 {
		w.AddBack = over
	}
	w.StatutoryWages = w.Wages + w.AddBack
	if w.Remuneration > 0 {
		w.ExcludedSharePPM = w.Excluded * payrollRateScale / w.Remuneration
	}
	return result, nil
}
