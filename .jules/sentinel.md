## 2025-05-19 - Symlink Traversal in File Writes
**Vulnerability:** The `ResolveWritePath` function validated the symlink's location within the workspace but failed to resolve the symlink's *target* before authorizing the write. This allowed an attacker to create a symlink inside the workspace pointing to a sensitive file outside (e.g., `/etc/passwd`) and overwrite it.
**Learning:** Checking that a path string is lexically within a directory is insufficient when the filesystem supports symlinks. The operating system resolves symlinks at access time, bypassing string-based checks.
**Prevention:** Always resolve the final path using `filepath.EvalSymlinks` (or `os.Readlink`) for existing files before performing security checks or file operations. Treat the filesystem as untrusted input.
