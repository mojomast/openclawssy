## 2024-05-22 - Scheduler Loop Optimization
**Learning:** Even with small N (100 jobs), sorting and `MarshalIndent` in the hot path of a scheduler loop caused significant overhead (~21% of total execution time including I/O). `json.Marshal` is measurably faster than `MarshalIndent`, and avoiding sorting in the check loop reduced CPU time by ~40%.
**Action:** Avoid sorting in `List` methods used by periodic checkers. Prefer `json.Marshal` for internal state files unless human readability is critical.

## 2024-05-24 - Zero-Allocation Scheduler Iteration
**Learning:** `ListUnsorted` allocated a full slice of jobs every tick, creating constant GC pressure proportional to job count. Replacing it with `Iterate` (callback-based iteration under lock) eliminated this allocation completely, improving throughput by ~2.3x for 1000 jobs. While this extends the lock duration to include processing time, the reduction in memory bandwidth and GC overhead is the dominant factor for performance in this single-writer/multi-reader scenario.
**Action:** Use iterator patterns instead of returning slices for hot-path collections where data is read-only or modified via specific methods. Be mindful of lock contention if the callback is slow.
