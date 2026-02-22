## 2025-05-19 - Symlink Traversal in File Writes
**Vulnerability:** The `ResolveWritePath` function validated the symlink's location within the workspace but failed to resolve the symlink's *target* before authorizing the write. This allowed an attacker to create a symlink inside the workspace pointing to a sensitive file outside (e.g., `/etc/passwd`) and overwrite it.
**Learning:** Checking that a path string is lexically within a directory is insufficient when the filesystem supports symlinks. The operating system resolves symlinks at access time, bypassing string-based checks.
**Prevention:** Always resolve the final path using `filepath.EvalSymlinks` (or `os.Readlink`) for existing files before performing security checks or file operations. Treat the filesystem as untrusted input.

## 2025-05-20 - Unprotected Read Access to Control Files
**Vulnerability:** The `ResolveReadPath` function allowed read access to sensitive control files (e.g., `master.key`, `secrets.enc`) because the `isProtectedControlPath` check was only applied during write operations.
**Learning:** Security controls often focus on preventing modification (write), but read access can be just as critical for confidentiality. Access control checks must be consistent across all operations (read/write/execute).
**Prevention:** Apply the same policy validation logic (e.g., protected path checks) to both read and write operations unless there is a specific reason to differ.

## 2025-05-21 - Globally Protected Filenames
**Vulnerability:** The `isProtectedControlPath` function only checked if sensitive files (like `master.key`) were inside the specific `.openclawssy` control directory. This allowed an agent to read or overwrite the master key if it was accidentally placed in the workspace root by a user.
**Learning:** Relying solely on directory location for security policies is fragile against user misconfiguration. Critical secrets should be protected by their filename regardless of location within the workspace.
**Prevention:** Enforce protection for critical filenames (e.g., `master.key`) globally across the entire workspace using `filepath.Base` checks before applying directory-specific logic.
