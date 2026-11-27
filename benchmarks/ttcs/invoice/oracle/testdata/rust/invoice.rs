pub struct Totals {
    pub subtotal: i64,
    pub discount: i64,
    pub due: i64,
}

pub fn invoice_totals(quantity: i64, unit_cents: i64, discount_percent: i64) -> Totals {
    let subtotal = quantity * unit_cents;
    let discount = (subtotal * discount_percent + 50) / 100;
    Totals {
        subtotal,
        discount,
        due: subtotal - discount,
    }
}
