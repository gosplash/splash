package main

type Totals struct {
	Subtotal int
	Discount int
	Due      int
}

func InvoiceTotals(quantity, unitCents, discountPercent int) Totals {
	subtotal := quantity * unitCents
	numerator := subtotal * discountPercent
	discount := numerator / 100
	if numerator%100 >= 50 {
		discount++
	}
	return Totals{Subtotal: subtotal, Discount: discount, Due: subtotal - discount}
}
