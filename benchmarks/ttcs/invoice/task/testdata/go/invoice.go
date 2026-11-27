package main

type Totals struct {
	Subtotal int
	Discount int
	Due      int
}

func InvoiceTotals(quantity, unitCents, discountPercent int) Totals {
	subtotal := quantity * unitCents
	discount := subtotal * discountPercent / 100
	return Totals{Subtotal: subtotal, Discount: discount, Due: subtotal - discount}
}
