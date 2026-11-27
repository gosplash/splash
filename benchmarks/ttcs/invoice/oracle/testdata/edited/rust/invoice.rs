pub struct Totals {
    pub subtotal: i64,
    pub discount: i64,
    pub due: i64,
}

pub fn invoice_totals(quantity: i64, unit_cents: i64, discount_percent: i64) -> Totals {
    let subtotal = quantity * unit_cents;
    let numerator = subtotal * discount_percent;
    let discount = numerator / 100 + i64::from(numerator % 100 >= 50);
    Totals {
        subtotal,
        discount,
        due: subtotal - discount,
    }
}
