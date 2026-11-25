package call

import (
	"math/big"
	"strconv"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func outsideLimit(sel Selection, limits *driver.ValueLimits) *routeRefusal {
	if sel.Release || limits == nil || sel.Value.Kind != semreg.ValueQuantity || sel.Value.Quantity == nil {
		return nil
	}
	value := sel.Value.Quantity
	if limits.Min != nil && limits.Min.Unit == value.Unit && compareDecimal(value.Number, limits.Min.Number) < 0 {
		return failed(limitReason("min", *limits.Min, limits.Origin))
	}
	if limits.Max != nil && limits.Max.Unit == value.Unit && compareDecimal(value.Number, limits.Max.Number) > 0 {
		return failed(limitReason("max", *limits.Max, limits.Origin))
	}
	return nil
}

func limitReason(bound string, quantity semreg.Quantity, origin driver.LimitOrigin) string {
	return ReasonOutsideLimit + " " + bound + "=" + quantity.Number.Coefficient + "e" + strconv.FormatInt(int64(quantity.Number.Exponent10), 10) + " " + string(quantity.Unit) + " origin=" + string(origin)
}

func compareDecimal(left, right semreg.Decimal) int {
	lv, lok := new(big.Int).SetString(left.Coefficient, 10)
	rv, rok := new(big.Int).SetString(right.Coefficient, 10)
	if !lok || !rok {
		return 0
	}
	switch {
	case left.Exponent10 > right.Exponent10:
		rv.Mul(rv, pow10(left.Exponent10-right.Exponent10))
	case right.Exponent10 > left.Exponent10:
		lv.Mul(lv, pow10(right.Exponent10-left.Exponent10))
	}
	return lv.Cmp(rv)
}

func pow10(exp int32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil)
}
