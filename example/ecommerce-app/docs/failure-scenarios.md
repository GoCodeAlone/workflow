# Failure Scenarios: Order Processing Pipeline

This document describes every failure path in the e-commerce order processing
pipeline, how the system handles each one, and what gaps remain compared to a
production deployment.

The pipeline is built from three layers that work together:

1. **Dynamic components** -- Go source loaded at runtime by the Yaegi
   interpreter. They simulate external services (inventory, payment, shipping).
2. **Processing steps** (`processing.step` modules) -- bridge layer that wraps a
   dynamic component with retry logic and fires state machine transitions based
   on the outcome.
3. **State machine** (`statemachine.engine`) -- enforces allowed transitions and
   records the order's current state atomically.

---

## State Overview

```
                                   HAPPY PATH
    +-----+   start_validation   +------------+   validation_passed   +-----------+
    | new | ------------------> | validating | --------------------> | validated |
    +-----+                      +------------+                       +-----------+
       |                              |                                    |
       | cancel_order                 | validation_failed                  | start_payment
       v                              v                                    v
  +-----------+                  +--------+                           +--------+
  | cancelled |                  | failed |                           | paying |
  +-----------+                  +--------+                           +--------+
       ^                                                               /     \
       | cancel_validated                           payment_approved  /       \ payment_declined
       |                                                             v         v
  +-----------+                                                 +------+  +----------------+
  | validated | <----(see above)                                | paid |  | payment_failed |
  +-----------+                                                 +------+  +----------------+
                                                                   |             |
                                                      start_shipping|     retry_payment
                                                                   v             |
                                                              +----------+       |
                                                              | shipping |<------+
                                                              +----------+  (back to paying)
                                                               /        \
                                              shipping_confirmed        shipping_failed
                                                             /            \
                                                            v              v
                                                       +---------+   +-------------+
                                                       | shipped |   | ship_failed |
                                                       +---------+   +-------------+
                                                            |               |
                                                deliver_order|        retry_shipping
                                                            v               |
                                                      +-----------+        |
                                                      | delivered |        |
                                                      +-----------+  (back to shipping)

    Terminal states: delivered, cancelled, failed
    Error state:     failed (isError: true)
```

---

## Scenario 1: Happy Path

Everything succeeds on the first attempt. This is the expected flow for
approximately 73% of orders (0.90 inventory x 0.85 payment x 0.95 shipping).

### State Diagram

```
  +-----+  start_validation  +------------+  validation_passed  +-----------+
  | new | -----------------> | validating | ------------------> | validated |
  +-----+                    +------------+                     +-----------+
                                                                     |
                                                          start_payment (event trigger)
                                                                     |
                                                                     v
  +-----------+  deliver_order  +---------+  shipping_confirmed  +----------+
  | delivered | <-------------- | shipped | <------------------- | shipping |
  +-----------+                 +---------+                      +----------+
                                                                     ^
                                                          start_shipping (event trigger)
                                                                     |
                                                                 +------+  payment_approved
                                                                 | paid | <--- paying
                                                                 +------+
```

### What Happens

1. **POST /api/orders** creates the order in `new` state.
2. The HTTP trigger fires `start_validation`, moving the order to `validating`.
3. The `step-validate` hook runs `inventory-checker.Execute()`.
   - Simulates 100-300ms delay.
   - Returns `{available: true, checked_items: [...], reserved_until: "..."}`.
4. Processing step sees `err == nil` and fires `validation_passed`.
   Order moves to `validated`.
5. The event trigger on `order.validated` fires `start_payment`.
   Order moves to `paying`.
6. The `step-payment` hook runs `payment-processor.Execute()`.
   - Simulates 50-150ms delay.
   - Returns `{transaction_id: "txn-...", status: "approved", last4: "4242"}`.
7. Processing step fires `payment_approved`. Order moves to `paid`.
8. The event trigger on `order.paid` fires `start_shipping`.
   Order moves to `shipping`.
9. The `step-shipping` hook runs `shipping-service.Execute()`.
   - Simulates 200-500ms delay.
   - Returns `{tracking_number: "TRK-...", carrier: "USPS", estimated_delivery: "..."}`.
10. Processing step fires `shipping_confirmed`. Order moves to `shipped`.
11. `deliver_order` transition moves the order to `delivered` (terminal).

### Notifications

The `notification-handler` fires at each milestone state (`validated`, `paid`,
`shipped`, `delivered`), logging what a real system would send as email/SMS.

### Production Considerations

In a real system each stage would also:

- Persist the transaction ID, tracking number, and reservation ID to the database.
- Emit structured audit log entries.
- Update inventory counts in a warehouse management system.
- Send actual emails via SendGrid/SES with branded templates.

---

## Scenario 2: Inventory Check Failure

Items are out of stock. The order fails permanently because inventory is a
binary check -- either the items exist in the warehouse or they do not.

### State Diagram

```
  +-----+  start_validation  +------------+  validation_failed  +--------+
  | new | -----------------> | validating | ------------------> | failed |
  +-----+                    +------------+                     +--------+
                                   |
                         inventory-checker returns
                         {available: false}
```

### What Happens

1. **POST /api/orders** creates the order in `new` state.
2. The HTTP trigger fires `start_validation`. Order moves to `validating`.
3. The `step-validate` hook runs `inventory-checker.Execute()`.
   - The 10% failure path fires (random roll >= 0.9).
   - Returns `{available: false, reason: "out_of_stock", failed_item: "..."}`.
   - This is a business-level result, **not** a Go error (`err == nil`).
4. The processing step fires `validation_failed` (compensate transition).
   Order moves to `failed` (terminal, `isError: true`).
5. Notification handler logs the failure.

### Gap: Business Result vs. Go Error

The current `processing.step` module fires the **success** transition whenever
`err == nil`, regardless of the result map contents. For inventory failure to
trigger `validation_failed` (the compensate transition), the processing step
must inspect the result -- for example, checking `result["available"] == false`.

If the processing step does not inspect the result, inventory failures would
incorrectly fire `validation_passed`. This is the most significant gap in the
current processing step abstraction. A production implementation would need
either:

- A configurable "success condition" expression (e.g., `result.available == true`).
- The dynamic component returning a Go error for business failures.
- A separate "result evaluator" hook between the executor and the transition.

### Production Considerations

- Inventory should be checked per-SKU with real-time warehouse API calls.
- A "reserve and hold" pattern prevents overselling: items are soft-reserved
  during checkout and hard-committed after payment.
- Partial availability (some items in stock, others not) requires split-order
  logic or customer notification with options.
- The failure should trigger a "back in stock" notification signup flow.

---

## Scenario 3: Payment Decline

The customer's card is declined. This is a permanent business failure for the
current payment attempt.

### State Diagram

```
  +-----+  start_validation  +------------+  validation_passed  +-----------+
  | new | -----------------> | validating | ------------------> | validated |
  +-----+                    +------------+                     +-----------+
                                                                     |
                                                              start_payment
                                                                     |
                                                                     v
                                                                +--------+
                                                                | paying |
                                                                +--------+
                                                                     |
                                                          payment_declined
                                                                     |
                                                                     v
                                                            +----------------+
                                                            | payment_failed |
                                                            +----------------+
```

### What Happens

1. Order passes through `new` -> `validating` -> `validated` (happy path so far).
2. Event trigger fires `start_payment`. Order moves to `paying`.
3. The `step-payment` hook runs `payment-processor.Execute()`.
   - The 10% decline path fires (random roll between 0.85 and 0.95).
   - Returns `{status: "declined", reason: "insufficient_funds"}`.
   - This is a business-level result, **not** a Go error (`err == nil`).
4. The processing step fires `payment_declined` (compensate transition).
   Order moves to `payment_failed`.
5. Notification handler logs the payment failure.

### What Happens Next

The state machine defines a `retry_payment` transition from `payment_failed`
back to `paying`, but **nothing in the current pipeline automatically triggers
it**. The order stays in `payment_failed` until:

- Manual intervention via the API (an admin fires `retry_payment`).
- A future scheduled job checks for stuck `payment_failed` orders.

### Gap: Same Business-Result Inspection Issue

Like Scenario 2, the processing step must distinguish between a successful
execution that returns a decline result and a Go error. The `{status: "declined"}`
result comes back with `err == nil`, so the processing step needs result
inspection logic to fire `payment_declined` instead of `payment_approved`.

### Production Considerations

- Payment declines should trigger a customer-facing email: "Update your payment
  method" with a deep link to retry.
- Limited automatic retries (e.g., 3 attempts over 24 hours) for soft declines
  (insufficient funds may clear).
- Hard declines (stolen card, invalid number) should not retry.
- The system should release the inventory reservation on permanent payment failure.
- PCI DSS compliance requires that card numbers never touch the application --
  use tokenized payment methods via Stripe Elements or PayPal JS SDK.
- Idempotency keys prevent double-charging on retries.

---

## Scenario 4: Payment Gateway Timeout (Transient Error, Auto-Retry)

The payment gateway is temporarily unreachable. The processing step retries
automatically with exponential backoff.

### State Diagram -- Retry Succeeds

```
  +-----------+  start_payment  +--------+  [timeout, retry 1]  +--------+
  | validated | -------------> | paying | --------------------> | paying |
  +-----------+                +--------+                       +--------+
                                                                    |
                                                     [success on retry]
                                                                    |
                                                         payment_approved
                                                                    |
                                                                    v
                                                                +------+
                                                                | paid |
                                                                +------+
```

### State Diagram -- All Retries Exhausted

```
  +-----------+  start_payment  +--------+  [timeout x3]  +----------------+
  | validated | -------------> | paying | -------------> | payment_failed |
  +-----------+                +--------+                 +----------------+
                                 ^ ^ ^
                                 | | |
                          attempt 1,2,3 (all fail with Go error)
```

### What Happens

1. Order reaches `paying` state via the normal path.
2. The `step-payment` hook runs `payment-processor.Execute()`.
   - The 5% timeout path fires (random roll >= 0.95).
   - Returns `nil, fmt.Errorf("payment gateway timeout")`.
   - This **is** a Go error, triggering the retry mechanism.
3. Processing step waits for exponential backoff:
   - Attempt 1 failed: wait 1000ms (base backoff).
   - Attempt 2: wait 2000ms.
   - Attempt 3 (if attempt 2 also fails): retries exhausted.
4. On retry success: fires `payment_approved`, order moves to `paid`.
5. On final failure: fires `payment_declined` (compensate), order moves to
   `payment_failed`.

### Retry Configuration

From `workflow.yaml` (`step-payment`):

```yaml
maxRetries: 2          # up to 3 total attempts (1 initial + 2 retries)
retryBackoffMs: 1000   # 1s base, doubles each retry (1s, 2s)
timeoutSeconds: 30     # per-attempt timeout
```

### Backoff Calculation

The `ProcessingStep.calculateBackoff()` method uses:

```
backoff = retryBackoffMs * 2^(attempt-1)
```

| Attempt | Backoff |
|---------|---------|
| 1 (first retry) | 1000ms |
| 2 (second retry) | 2000ms |

### Production Considerations

- Add jitter to backoff to prevent thundering herd: `backoff * (0.5 + random(0.5))`.
- Implement circuit breaker: after N consecutive failures, stop attempting the
  gateway entirely and fail fast for a cooldown period.
- Use a dead letter queue for orders that exhaust retries so they can be
  reprocessed when the gateway recovers.
- Log the transient error with correlation IDs for troubleshooting.
- Set up alerts on retry rate: a spike indicates gateway degradation.

---

## Scenario 5: Shipping Label Failure

The shipping provider is temporarily unavailable. Similar retry logic to payment
but with different backoff parameters.

### State Diagram -- Retry Succeeds

```
  +------+  start_shipping  +----------+  [error, retry]  +----------+
  | paid | ---------------> | shipping | ----------------> | shipping |
  +------+                  +----------+                   +----------+
                                                                |
                                                  [success on retry]
                                                                |
                                                     shipping_confirmed
                                                                |
                                                                v
                                                          +---------+
                                                          | shipped |
                                                          +---------+
                                                                |
                                                        deliver_order
                                                                |
                                                                v
                                                          +-----------+
                                                          | delivered |
                                                          +-----------+
```

### State Diagram -- All Retries Exhausted

```
  +------+  start_shipping  +----------+  [error x3]  +-------------+
  | paid | ---------------> | shipping | ------------> | ship_failed |
  +------+                  +----------+               +-------------+
                              ^ ^ ^
                              | | |
                       attempt 1,2,3 (all fail with Go error)
```

### What Happens

1. Order reaches `shipping` state after successful payment.
2. The `step-shipping` hook runs `shipping-service.Execute()`.
   - The 5% failure path fires (random roll >= 0.95).
   - Returns `nil, fmt.Errorf("shipping provider unavailable")`.
   - This is a Go error, triggering retries.
3. Processing step retries with exponential backoff:
   - Attempt 1 failed: wait 2000ms (base backoff).
   - Attempt 2: wait 4000ms.
4. On retry success: fires `shipping_confirmed`, order moves to `shipped`.
5. On final failure: fires `shipping_failed` (compensate), order moves to
   `ship_failed`.

### Retry Configuration

From `workflow.yaml` (`step-shipping`):

```yaml
maxRetries: 2          # up to 3 total attempts
retryBackoffMs: 2000   # 2s base, doubles each retry (2s, 4s)
timeoutSeconds: 30     # per-attempt timeout
```

### Recovery Path

Like payment, the state machine defines `retry_shipping` from `ship_failed`
back to `shipping`, but nothing triggers it automatically. Manual intervention
or a scheduled retry job is needed.

### Production Considerations

- Shipping failures are less time-sensitive than payment: a 15-minute retry
  window is acceptable.
- Multi-carrier fallback: if USPS is down, try UPS or FedEx.
- Rate shopping: select the cheapest carrier that meets the delivery window.
- The system should notify the customer of shipping delays.
- Package dimensions and weight should be calculated for accurate label generation.
- Returns processing needs a reverse shipping flow (not implemented).

---

## Scenario 6: Order Cancellation

The customer cancels their order before payment processing begins.

### State Diagram -- Cancel from New

```
  +-----+  cancel_order  +-----------+
  | new | -------------> | cancelled |
  +-----+                +-----------+
```

### State Diagram -- Cancel from Validated

```
  +-----+  start_validation  +------------+  validation_passed  +-----------+
  | new | -----------------> | validating | ------------------> | validated |
  +-----+                    +------------+                     +-----------+
                                                                     |
                                                              cancel_validated
                                                                     |
                                                                     v
                                                                +-----------+
                                                                | cancelled |
                                                                +-----------+
```

### What Happens

1. **Cancel from `new`**: Customer cancels immediately after placing the order,
   before the validation trigger fires. The `cancel_order` transition moves the
   order directly to `cancelled`.

2. **Cancel from `validated`**: Inventory has been checked and reserved, but
   payment has not started. The `cancel_validated` transition moves the order to
   `cancelled`.

3. Both cancellation paths result in the terminal `cancelled` state.
   The notification handler logs the cancellation.

### Cancellation Windows

The state machine only allows cancellation from two states:

| From State | Transition | Allowed? |
|-----------|-----------|----------|
| `new` | `cancel_order` | Yes |
| `validated` | `cancel_validated` | Yes |
| `validating` | -- | No (validation in progress) |
| `paying` | -- | No (payment in progress) |
| `paid` | -- | No (already charged) |
| `shipping` | -- | No (label being generated) |
| `shipped` | -- | No (package in transit) |

### Gap: Race Condition

There is a race between the event trigger (which fires `start_payment` on
`order.validated`) and a cancellation request. If the customer sends a cancel
request while the system is processing the `order.validated` event:

- If `cancel_validated` fires first: order is cancelled (correct).
- If `start_payment` fires first: order moves to `paying` and can no longer
  be cancelled without a refund flow.

The state machine's atomic transition enforcement prevents both from succeeding,
but the outcome depends on which request arrives first.

### Production Considerations

- Cancellation after payment requires a refund flow:
  `paid` -> `refunding` -> `refunded` (new states needed).
- Cancellation after shipping requires a return-and-refund flow.
- The inventory reservation created during validation should be released on
  cancellation.
- Customer should receive a cancellation confirmation email with any refund
  details.
- Consider a "cancellation requested" intermediate state with admin approval
  for high-value orders.

---

## Summary: Failure Probability

Based on the simulated rates in the dynamic components:

| Scenario | Probability | Outcome |
|----------|------------|---------|
| Full happy path | ~73% (0.90 x 0.85 x 0.95) | `delivered` |
| Inventory failure | ~10% | `failed` |
| Payment decline | ~9% (0.90 x 0.10) | `payment_failed` |
| Payment timeout (retried) | ~4.5% (0.90 x 0.05) | `paid` (after retry) or `payment_failed` |
| Shipping failure (retried) | ~3.9% (0.90 x 0.85 x 0.05) | `shipped` (after retry) or `ship_failed` |

Note: These probabilities are per-attempt. With retries, the effective success
rate for transient errors is higher (e.g., a 5% timeout rate per attempt with
2 retries gives ~0.0125% chance of all attempts failing).

---

## Key Architectural Insight

The processing step module (`module/processing_step.go`) makes a deliberate
distinction between two types of failures:

1. **Go errors** (`err != nil`): Treated as transient. The processing step
   retries with exponential backoff up to `maxRetries` times. Only after all
   retries are exhausted does it fire the compensate transition.

2. **Business results** (`err == nil` with failure data): Currently treated the
   same as success by the processing step (it fires the success transition).
   This means the system needs additional logic to inspect result data and
   determine whether to fire the success or compensate transition.

This distinction works well for transient infrastructure failures (gateway
timeouts, network errors) but requires enhancement to handle business-level
rejections (declined payments, out-of-stock inventory) that return structured
data rather than Go errors.
