# Fix invoice discount rounding

The invoice calculation currently truncates the discount. Update it to round the discount to the nearest cent, with exact half-cent ties rounded up. Round once on the subtotal, after multiplying the quantity by the unit price. The amount due is the subtotal minus the rounded discount.

Inputs are nonnegative integers. Quantity is at most 10,000, unit price is at most 2,000 cents, and the discount is a whole percentage from 0 to 100. Results are integer cents. Validation outside these bounds is outside the task.

Edit only the starter invoice file for your language. Keep the existing function and result record available to callers. Preserve the existing exact-cent and no-discount behavior.

| Quantity | Unit price in cents | Discount percent | Subtotal | Discount | Amount due |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 3 | 200 | 10 | 600 | 60 | 540 |
| 3 | 199 | 0 | 597 | 0 | 597 |
