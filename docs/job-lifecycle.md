# Job Lifecycle

A job represents a unit of work submitted by a project. Every job belongs to exactly one project and one queue.

## Statuses

| Status | Meaning | Terminal |
| --- | --- | --- |
| `pending` | Accepted and eligible for dispatch. | No |
| `scheduled` | Accepted but not eligible for dispatch until `scheduled_at`. | No |
| `running` | Claimed by a worker and currently executing. | No |
| `succeeded` | Completed successfully. | Yes |
| `failed` | Exhausted its retry budget or failed permanently. | Yes |
| `cancelled` | Cancelled before successful completion. | Yes |

## Transitions

```text
                 +-----------+
                 |  pending  |
                 +-----+-----+
                       |
          +------------+------------+
          |                         |
          v                         v
   +-----------+              +-----------+
   | scheduled |              | cancelled |
   +-----+-----+              +-----------+
         |                          ^
         |                          |
         v                          |
   +-----------+                    |
   |  running  |--------------------+
   +-----+-----+
         |
         +-------------------+
         |                   |
         v                   v
   +-----------+       +-----------+
   | succeeded |       |  failed   |
   +-----------+       +-----------+
```

Allowed transitions:

| From | To |
| --- | --- |
| `pending` | `scheduled`, `running`, `cancelled` |
| `scheduled` | `running`, `cancelled` |
| `running` | `succeeded`, `failed`, `scheduled`, `cancelled` |
| `succeeded` | — |
| `failed` | — |
| `cancelled` | — |

`running -> scheduled` is used for a retry: the failed attempt is recorded as a separate execution, and the job becomes eligible for another attempt after the retry delay.

## Submission

When a job is submitted:

1. If the client sent an `Idempotency-Key` header, the key and a request fingerprint are stored with the job. Replaying the same key and request returns the original job; reusing the key with a different request is rejected with `409 Conflict`.
2. The job is created with status `pending`, or `scheduled` when `scheduled_at` is in the future. Omitted queue, priority, and max-attempt values come from the project's settings.
3. An `outbox_messages` row is created in the same transaction.
4. The outbox dispatcher writes to the `relayforge_jobs` PostgreSQL notification channel and marks the outbox message processed in the same transaction.
5. A worker claims the job, changes it to `running`, and creates an execution attempt.

The job and outbox insert share a transaction so a job cannot be committed without a dispatch record. The outbox dispatcher claims messages with `FOR UPDATE SKIP LOCKED`, allowing multiple dispatchers to run safely.

## Executions and retries

Each attempt creates one row in `executions`. The execution records the worker, attempt number, start and finish times, result, and error message.

- A successful attempt sets the execution to `succeeded` and the job to `succeeded`.
- A failed attempt sets the execution to `failed`.
- If attempts remain, the job moves back to `scheduled` using a retry delay.
- Once `attempts >= max_attempts`, the job moves to terminal `failed`.

## Cancellation

Cancellation is terminal. Jobs can be cancelled while `pending`, `scheduled`, or `running`. A worker should check cancellation before reporting a successful completion.
