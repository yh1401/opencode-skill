# Error Pattern Reference

This file documents known error patterns used by `root_cause.py` for log analysis.
Patterns are organized by category.

## Code Bugs

| Pattern ID | Java Exception | Indicator |
|-----------|----------------|-----------|
| null_pointer | NullPointerException | `java.lang.NullPointerException` |
| stack_overflow | StackOverflowError | `StackOverflowError` |
| array_bounds | IndexOutOfBoundsException | `IndexOutOfBoundsException` |
| illegal_arg | IllegalArgumentException | `IllegalArgumentException` |
| class_cast | ClassCastException | `ClassCastException` |

## Resource Exhaustion

| Pattern ID | Name | Indicator |
|-----------|------|-----------|
| out_of_memory | OutOfMemoryError | `java.lang.OutOfMemoryError` / `OOM` |
| disk_full | Disk Space Full | `No space left on device` |
| oom_killer | System OOM | `oom-killer` / `Memory cgroup` |

## Network & Connectivity

| Pattern ID | Name | Indicator |
|-----------|------|-----------|
| connection_timeout | Connection Timeout | `connect timeout` / `Read timed out` |
| connection_refused | Connection Refused | `Connection refused` |
| connection_reset | Connection Reset | `Connection reset` |
| service_unavailable | Service Unavailable | `503` / `circuit breaker` |

## Database

| Pattern ID | Name | Indicator |
|-----------|------|-----------|
| sql_error | SQLException | `java.sql.SQLException` |
| deadlock | Database Deadlock | `deadlock` |
| pool_timeout | Connection Pool Exhausted | `pool timeout` / `HikariPool` |

## Deployment

| Pattern ID | Name | Indicator |
|-----------|------|-----------|
| class_not_found | ClassNotFoundException | `ClassNotFoundException` |
| missing_dependency | NoClassDefFoundError | `NoClassDefFoundError` |
| version_mismatch | NoSuchMethodError | `NoSuchMethodError` |

## Configuration

| Pattern ID | Name | Indicator |
|-----------|------|-----------|
| file_not_found | FileNotFoundException | `FileNotFoundException` |
| permission_denied | Permission Denied | `Permission denied` |
| config_error | Configuration Error | `Configuration error` / `Invalid config` |
