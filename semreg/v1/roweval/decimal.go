package roweval

import (
	"fmt"
	"math/big"
	"strings"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// The rounding modes a Convert.Round may name.
const (
	RoundTowardZero = "toward_zero"
	RoundHalfUp     = "half_up"
	RoundHalfEven   = "half_even"
)

// maxDecimalExponent is the largest exponent magnitude of a v1.Decimal.
const maxDecimalExponent = 18

func pow10(e int64) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(e), nil) }

func ratOf(d v1.Decimal) (*big.Rat, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	n, ok := new(big.Int).SetString(d.Coefficient, 10)
	if !ok {
		return nil, fmt.Errorf("decimal coefficient %q", d.Coefficient)
	}
	r := new(big.Rat).SetInt(n)
	if d.Exponent10 >= 0 {
		return r.Mul(r, new(big.Rat).SetInt(pow10(int64(d.Exponent10)))), nil
	}
	return r.Quo(r, new(big.Rat).SetInt(pow10(int64(-d.Exponent10)))), nil
}

// decimalOf turns an exact rational into a Decimal at the finest exponent the
// type holds. A result that is not exact is an error unless a rounding mode is
// named.
func decimalOf(r *big.Rat, mode string) (v1.Decimal, error) {
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(pow10(18)))
	n := new(big.Int)
	if scaled.IsInt() {
		n.Set(scaled.Num())
	} else {
		switch mode {
		case "":
			return v1.Decimal{}, fmt.Errorf("result is not exact and no rounding is named")
		case RoundTowardZero:
			n.Quo(scaled.Num(), scaled.Denom())
		case RoundHalfUp, RoundHalfEven:
			q, rem := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
			twice := new(big.Int).Abs(rem)
			twice.Lsh(twice, 1)
			cmp := twice.Cmp(scaled.Denom())
			up := cmp > 0 || (cmp == 0 && (mode == RoundHalfUp || q.Bit(0) == 1))
			if up {
				if scaled.Sign() < 0 {
					q.Sub(q, big.NewInt(1))
				} else {
					q.Add(q, big.NewInt(1))
				}
			}
			n = q
		default:
			return v1.Decimal{}, fmt.Errorf("unknown rounding %q", mode)
		}
	}
	exp := int32(-18)
	if n.Sign() == 0 {
		return v1.Decimal{Coefficient: "0"}, nil
	}
	ten := big.NewInt(10)
	for exp < 18 {
		q, rem := new(big.Int).QuoRem(n, ten, new(big.Int))
		if rem.Sign() != 0 {
			break
		}
		n, exp = q, exp+1
	}
	d := v1.Decimal{Coefficient: n.String(), Exponent10: exp}
	if err := d.Validate(); err != nil {
		return v1.Decimal{}, fmt.Errorf("result does not fit a decimal: %w", err)
	}
	return d, nil
}

// decimalText is the canonical plain text of a Decimal.
func decimalText(d v1.Decimal) string {
	r, err := ratOf(d)
	if err != nil {
		return ""
	}
	if r.IsInt() {
		return r.Num().String()
	}
	s := r.FloatString(18)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// affineOf applies value = x × Num/Den × 10^exponent + Add exactly.
func affineOf(a Affine, x v1.Decimal, exponent int64, round string) (v1.Decimal, error) {
	if a.Den == 0 {
		return v1.Decimal{}, fmt.Errorf("affine denominator is zero")
	}
	r, err := ratOf(x)
	if err != nil {
		return v1.Decimal{}, err
	}
	r.Mul(r, big.NewRat(a.Num, a.Den))
	// The limit applies to the result, not to the scaling exponent alone. A power
	// of ten is built only when it is within the digits of the operands plus two
	// decimal exponent ranges (the operand's own exponent supplies headroom for
	// the scaling): a result that is a Decimal cannot need more, so the work
	// stays bounded without refusing a representable result.
	if bound := int64(2*maxDecimalExponent + len(x.Coefficient) + len(a.Add.Coefficient) +
		len(big.NewInt(a.Num).String()) + len(big.NewInt(a.Den).String())); exponent < -bound || exponent > bound {
		return v1.Decimal{}, fmt.Errorf("scaling by 10^%d cannot give a decimal", exponent)
	}
	if exponent >= 0 {
		r.Mul(r, new(big.Rat).SetInt(pow10(exponent)))
	} else {
		r.Quo(r, new(big.Rat).SetInt(pow10(-exponent)))
	}
	if a.Add != (v1.Decimal{}) {
		add, err := ratOf(a.Add)
		if err != nil {
			return v1.Decimal{}, err
		}
		r.Add(r, add)
	}
	return decimalOf(r, round)
}

func compareDecimal(a, b v1.Decimal) (int, error) {
	x, err := ratOf(a)
	if err != nil {
		return 0, err
	}
	y, err := ratOf(b)
	if err != nil {
		return 0, err
	}
	return x.Cmp(y), nil
}

// boundsReason checks x against min, max and step. The step counts from the
// minimum when there is one and from zero otherwise.
func boundsReason(b Bounds, x v1.Decimal) (Reason, string) {
	xr, err := ratOf(x)
	if err != nil {
		return ReasonConversion, err.Error()
	}
	base := new(big.Rat)
	if b.Min != nil {
		m, err := ratOf(*b.Min)
		if err != nil {
			return ReasonConversion, err.Error()
		}
		if xr.Cmp(m) < 0 {
			return ReasonOutsideDomain, "below the minimum"
		}
		base = m
	}
	if b.Max != nil {
		m, err := ratOf(*b.Max)
		if err != nil {
			return ReasonConversion, err.Error()
		}
		if xr.Cmp(m) > 0 {
			return ReasonOutsideDomain, "above the maximum"
		}
	}
	if b.Step != nil {
		s, err := ratOf(*b.Step)
		if err != nil {
			return ReasonConversion, err.Error()
		}
		if s.Sign() <= 0 {
			return ReasonConversion, "step is not positive"
		}
		q := new(big.Rat).Sub(xr, base)
		q.Quo(q, s)
		if !q.IsInt() {
			return ReasonOffStep, "not a multiple of the step"
		}
	}
	return "", ""
}
