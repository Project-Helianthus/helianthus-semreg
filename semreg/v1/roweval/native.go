package roweval

import (
	"fmt"
	"math/big"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Native is the native evaluation of one matched row. It is kept whether or not
// a fact candidate follows.
type Native struct {
	RowID       string
	Unit        string
	Raw         uint64
	Value       *v1.Value
	Disposition string
	Reason      Reason
	Detail      string
	Evidence    []v1.EvidenceRef
}

type convEnv[S any] struct {
	table     Table[S]
	desc      Descriptors[S]
	payload   []byte
	qualifier Qualifiers
	inputs    DetachedInputs
	row       RowID
	dir       Direction
	citations []v1.EvidenceRef
}

func orderOf(m map[string]Order, id string) (Order, bool) {
	o, ok := m[id]
	return o, ok && o.Decode != nil && o.Encode != nil
}

func codecOf[S any](d Descriptors[S], id string) (Codec, bool) {
	c, ok := d.Codecs[id]
	return c, ok && c.Decode != nil && c.Encode != nil
}

func identifiers[S any](d Descriptors[S], l Layout) (Codec, Order, Order, Order, string) {
	c, ok := codecOf(d, l.Codec)
	if !ok {
		return Codec{}, Order{}, Order{}, Order{}, "codec " + l.Codec
	}
	by, ok := orderOf(d.ByteOrders, l.ByteOrder)
	if !ok {
		return Codec{}, Order{}, Order{}, Order{}, "byte order " + l.ByteOrder
	}
	wo, ok := orderOf(d.WordOrders, l.WordOrder)
	if !ok {
		return Codec{}, Order{}, Order{}, Order{}, "word order " + l.WordOrder
	}
	bi, ok := orderOf(d.BitOrders, l.BitOrder)
	if !ok {
		return Codec{}, Order{}, Order{}, Order{}, "bit order " + l.BitOrder
	}
	return c, by, wo, bi, ""
}

func fieldBytes(l Layout) int { return (int(l.BitOffset) + int(l.Width) + 7) / 8 }

// extract reads the raw field of a layout from a payload.
func extract[S any](d Descriptors[S], l Layout, payload []byte) (raw uint64, c Codec, reason Reason, detail string) {
	c, by, wo, bi, missing := identifiers(d, l)
	if missing != "" {
		return 0, Codec{}, ReasonUnknownID, "unknown " + missing
	}
	if l.Width < 1 || l.Width > 64 {
		return 0, Codec{}, ReasonExtent, fmt.Sprintf("width %d", l.Width)
	}
	if fieldBytes(l) > len(payload) {
		return 0, Codec{}, ReasonShortPayload, fmt.Sprintf("field ends at byte %d, payload has %d", fieldBytes(l), len(payload))
	}
	for i := uint16(0); i < l.Width; i++ {
		p := int(l.BitOffset + i)
		if payload[p/8]>>(uint(p)%8)&1 == 1 {
			raw |= 1 << i
		}
	}
	var err error
	for _, o := range []Order{bi, by, wo} {
		if raw, err = o.Decode(raw, l.Width); err != nil {
			return 0, Codec{}, ReasonDecode, err.Error()
		}
	}
	return raw, c, "", ""
}

// place writes the raw field of a layout into a payload of the given length.
func place[S any](d Descriptors[S], l Layout, number v1.Decimal, length int) ([]byte, Reason, string) {
	c, by, wo, bi, missing := identifiers(d, l)
	if missing != "" {
		return nil, ReasonUnknownID, "unknown " + missing
	}
	if l.Width < 1 || l.Width > 64 {
		return nil, ReasonExtent, fmt.Sprintf("width %d", l.Width)
	}
	if length < 0 {
		length = fieldBytes(l)
	}
	if fieldBytes(l) > length {
		return nil, ReasonExtent, fmt.Sprintf("field ends at byte %d, payload has %d", fieldBytes(l), length)
	}
	raw, err := c.Encode(number, l.Width)
	if err != nil {
		return nil, ReasonEncode, err.Error()
	}
	for _, o := range []Order{wo, by, bi} {
		if raw, err = o.Encode(raw, l.Width); err != nil {
			return nil, ReasonEncode, err.Error()
		}
	}
	out := make([]byte, length)
	for i := uint16(0); i < l.Width; i++ {
		if raw>>i&1 == 1 {
			p := int(l.BitOffset + i)
			out[p/8] |= 1 << (uint(p) % 8)
		}
	}
	return out, "", ""
}

func quantityOf(n v1.Decimal, unit v1.DefinitionID) v1.Value {
	return v1.Value{Kind: v1.ValueQuantity, Quantity: &v1.Quantity{Number: n, Unit: unit}}
}

func parseDecimal(text string) (v1.Decimal, error) {
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		return v1.Decimal{}, fmt.Errorf("%q is not a number", text)
	}
	return decimalOf(r, "")
}

// apply runs one Convert: the numeric conversion, then the symbol mapping, then
// the hook.
func (e *convEnv[S]) apply(c Convert, v v1.Value, out v1.DefinitionID) (v1.Value, Reason, string) {
	if c.Numeric != nil {
		if v.Kind != v1.ValueQuantity || v.Quantity == nil {
			return v1.Value{}, ReasonConversion, "numeric conversion needs a quantity"
		}
		var exp int64
		if c.Numeric.ExponentRow != "" {
			var reason Reason
			var detail string
			if exp, reason, detail = e.exponent(c.Numeric.ExponentRow); reason != "" {
				return v1.Value{}, reason, detail
			}
		}
		n, err := affineOf(*c.Numeric, v.Quantity.Number, exp, c.Round)
		if err != nil {
			return v1.Value{}, ReasonConversion, err.Error()
		}
		v = quantityOf(n, out)
	} else if v.Kind == v1.ValueQuantity && v.Quantity != nil {
		v = quantityOf(v.Quantity.Number, out)
	}
	if len(c.Symbols) > 0 {
		switch {
		case v.Kind == v1.ValueQuantity && v.Quantity != nil:
			token, ok := c.Symbols[decimalText(v.Quantity.Number)]
			if !ok {
				return v1.Value{}, ReasonConversion, "no symbol for " + decimalText(v.Quantity.Number)
			}
			v = v1.Value{Kind: v1.ValueSymbol, Symbol: &v1.Symbol{Namespace: out, Token: token, Known: true}}
		case v.Kind == v1.ValueSymbol && v.Symbol != nil:
			var native string
			for k, token := range c.Symbols {
				if token == v.Symbol.Token {
					if native != "" {
						return v1.Value{}, ReasonAmbiguous, "two native values for symbol " + token
					}
					native = k
				}
			}
			if native == "" {
				return v1.Value{}, ReasonConversion, "no native value for symbol " + v.Symbol.Token
			}
			n, err := parseDecimal(native)
			if err != nil {
				return v1.Value{}, ReasonConversion, err.Error()
			}
			v = quantityOf(n, out)
		default:
			return v1.Value{}, ReasonConversion, "symbol conversion needs a quantity or a symbol"
		}
	}
	if c.Hook != "" {
		h, ok := e.table.Hooks[c.Hook]
		if !ok || h == nil {
			return v1.Value{}, ReasonUnknownHook, "hook " + c.Hook
		}
		dv, reason := h(e.inputs, e.row, e.dir, v)
		if reason != "" {
			return v1.Value{}, reason, "hook " + c.Hook
		}
		e.citations = append(e.citations, dv.Citations...)
		v = dv.Value
	}
	return v, "", ""
}

// exponent reads a signed power-of-ten row of the same observation set.
func (e *convEnv[S]) exponent(id string) (int64, Reason, string) {
	row, reason, detail := rowByID(e.table, id, e.qualifier)
	if reason != "" {
		return 0, reason, detail
	}
	raw, c, reason, detail := extract(e.desc, row.Wire, e.payload)
	if reason != "" {
		return 0, reason, "exponent row " + id + ": " + detail
	}
	if d, ok := row.Wire.Sentinels[raw]; ok {
		return 0, ReasonSentinel, "exponent row " + id + ": " + d
	}
	n, err := c.Decode(raw, row.Wire.Width)
	if err != nil {
		return 0, ReasonDecode, "exponent row " + id + ": " + err.Error()
	}
	if r, detail := boundsReason(row.Domain, n); r != "" {
		return 0, r, "exponent row " + id + ": " + detail
	}
	rat, err := ratOf(n)
	if err != nil || !rat.IsInt() || !rat.Num().IsInt64() {
		return 0, ReasonConversion, "exponent row " + id + " is not an integer"
	}
	return rat.Num().Int64(), "", ""
}

// evalNative evaluates one matched native row on a payload.
func evalNative[S any](t Table[S], a Acquired[S], row NativeRow[S], inputs DetachedInputs) (Native, *convEnv[S]) {
	n := Native{RowID: row.ID, Unit: row.Unit, Evidence: append([]v1.EvidenceRef(nil), a.Template.Evidence...)}
	env := &convEnv[S]{table: t, desc: a.Descriptors, payload: a.Payload, qualifier: a.Qualifiers, inputs: inputs, row: RowID(row.ID), dir: Forward}
	fail := func(r Reason, d string) (Native, *convEnv[S]) {
		n.Reason, n.Detail = r, d
		return n, env
	}
	if a.Descriptors.Extent != nil {
		if length, ok := a.Descriptors.Extent(row.At); ok && len(a.Payload) < length {
			return fail(ReasonShortPayload, fmt.Sprintf("payload has %d bytes, the selector fixes %d", len(a.Payload), length))
		}
	}
	raw, codec, reason, detail := extract(a.Descriptors, row.Wire, a.Payload)
	n.Raw = raw
	if reason != "" {
		return fail(reason, detail)
	}
	if d, ok := row.Wire.Sentinels[raw]; ok {
		n.Disposition = d
		return fail(ReasonSentinel, d)
	}
	num, err := codec.Decode(raw, row.Wire.Width)
	if err != nil {
		return fail(ReasonDecode, err.Error())
	}
	unit := v1.DefinitionID(row.Unit)
	v, reason, detail := env.apply(row.Decode, quantityOf(num, unit), unit)
	if reason != "" {
		return fail(reason, detail)
	}
	if v.Kind == v1.ValueQuantity && v.Quantity != nil {
		if r, d := boundsReason(row.Domain, v.Quantity.Number); r != "" {
			n.Value = &v
			return fail(r, d)
		}
	}
	n.Value = &v
	return n, env
}
