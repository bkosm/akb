# rclone Setup

rclone is required only if you use remote-backed knowledge bases (S3, GCS, SFTP, etc.). Local-directory KBs work without it.

## Installing rclone

> **Do not install rclone via Homebrew on macOS.** The Homebrew formula builds
> rclone without CGO (`CGO_ENABLED=0`), which strips out the `cmount` build tag.
> This means `rclone mount` (FUSE) **does not work** — it will error with:
>
> ```
> rclone mount is not supported on MacOS when rclone is installed via Homebrew
> ```
>
> `rclone nfsmount` (NFS) does work with the Homebrew build, but for full
> mount method support, use the official binary.

**Recommended:** use the installer script (detects OS and architecture automatically):

```bash
# installs to /usr/local/bin (default)
curl -fsSL https://raw.githubusercontent.com/bkosm/akb/main/bin/install-rclone.sh | bash

# user-local install, no sudo required
curl -fsSL https://raw.githubusercontent.com/bkosm/akb/main/bin/install-rclone.sh | bash -s -- "$HOME/.bin"
```

If you use a custom directory, make sure it is on your `PATH` (add to `~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$HOME/.bin:$PATH"
```

If you already have the Homebrew version installed, a user-local binary in
`$HOME/.bin` will take precedence as long as `$HOME/.bin` appears before
`/opt/homebrew/bin` in your `$PATH`.

## Mount methods

Remote KBs can be mounted using two strategies, controlled by the
`mount_method` field in the KB config:

| Value | rclone command | FUSE required | Notes |
|-------|---------------|--------------|-------|
| `""` (auto) | depends | depends | Prefers FUSE if available, falls back to NFS |
| `"fuse"` | `rclone mount` | **yes** | Best performance; requires a FUSE library |
| `"nfs"` | `rclone nfsmount` | **no** | Zero dependencies beyond rclone; slower writes on macOS |

## FUSE on macOS

On macOS there are two FUSE implementations. rclone (via the [cgofuse](https://github.com/winfsp/cgofuse)
library) probes them in this fixed order:

1. `/usr/local/lib/libfuse.2.dylib` — **macFUSE** (kernel extension, requires System Settings approval + reboot)
2. `/usr/local/lib/libosxfuse.2.dylib` — osxfuse legacy
3. `/usr/local/lib/libfuse-t.dylib` — **FUSE-T** (userspace via NFS, no kernel extension)

**You cannot select between them at runtime.** cgofuse loads the first dylib
it finds. This creates a problem when both are installed:

- If macFUSE's dylib exists but its kernel extension hasn't been authorized
  in System Settings → Privacy & Security, `rclone mount` will **fail** even
  though FUSE-T is also installed — because macFUSE's dylib is found first.
- The fix is to **uninstall macFUSE** if you want to use FUSE-T, or
  **authorize the macFUSE kext** in System Settings.

**Recommendation:** install FUSE-T only (`brew install --cask fuse-t`) — it
doesn't require kernel extensions or reboots.

## FUSE on Linux

Install FUSE 3:

```bash
sudo apt install fuse3   # Debian/Ubuntu
sudo dnf install fuse3   # Fedora
```

## NFS mount (no FUSE)

`rclone nfsmount` spins up a local NFS server and mounts it — no FUSE library
needed at all. This is the zero-setup option that works everywhere, including
with the Homebrew rclone build. Trade-off: writes are significantly slower on
macOS (~60s vs sub-second for FUSE-T).

The `auto` mount method (the default when `mount_method` is omitted) handles
this transparently: it checks for FUSE availability and falls back to NFS if
FUSE is not installed.

## Multiple AKB processes

Each AKB server process tracks only the rclone mounts that it starts itself.
`akb://kbs` reports this local process view; it is not a global owner registry
for every mount on the host.

Do not run two AKB processes with the same remote KB configured to the same
local mountpoint. Sharing the same remote storage can work with the remote-write
caveats below, but concurrent AKB processes should use different local
mountpoints.

When a remote KB starts, AKB checks whether the configured local mountpoint is
already mounted. If that mountpoint is already mounted but is not registered in
the current AKB process, AKB treats it as a conflict and refuses to unmount it
automatically. The MCP server keeps running, but that KB appears in `akb://kbs`
with `mount_status: "failed"` and a `mount_error` describing the conflict.

AKB does not currently borrow existing mounts from another process. A borrowed
process could use the files, but it could not track the owner process's rclone
child, provide the same `use_kb sync` assurance, or safely unmount the owner
process's mount.

AKB notices mount problems only through local symptoms:

- its tracked rclone child exits unexpectedly
- `akb://kbs` checks the OS mount table and sees the path is not mounted
- `use_kb sync` checks the mount and returns an error
- file tools fail or see the underlying unmounted directory

There is no automatic retry or remount loop after startup. To recover from a
mountpoint conflict, stop the other owner process, change one AKB process to use
a unique local mountpoint, then call `use_kb` with `action: "mount"` or restart
the MCP server. If a mount was disrupted while writes were buffered, those
writes may not have received AKB's write-back wait.

### NFS silly-rename files

When using `rclone nfsmount`, files whose names start with `.nfs` can appear in
the mount directory. These are NFS "silly rename" placeholders, not Finder
metadata files or AKB-created KB content. NFS creates them when a file is
deleted while a process still has it open, so the process can keep using the
file until it closes the handle.

These files usually disappear after the owning process closes the file. If they
persist, look for an open handle or stale NFS client/server state rather than
treating them like `.DS_Store` or `._*` cleanup artifacts. For example,
`lsof <mount-dir>/.nfs...` can inspect a specific file; `lsof +D <mount-dir>`
can scan the whole mount, but may be slow on large KBs.

AKB does not automatically sweep `.nfs*` files. Deleting them can race active
processes, fail while the file is busy, or remove the evidence needed to debug
which process is holding the file open. Avoid intentionally naming KB files
with a `.nfs` prefix on NFS mounts.

## macOS metadata files

macOS and some FUSE drivers may create AppleDouble sidecar files (`._*`) and
`.DS_Store` files when writing through mounted directories. AKB treats these as
disposable metadata artifacts.

On macOS, AKB passes rclone metadata-suppression flags where supported
(`noappledouble`, `noapplexattr`). Some FUSE drivers can still leak sidecars, so
AKB also removes `._*` and `.DS_Store` files from remote mounts before
`use_kb sync` and before graceful unmount write-back waits.

Do not store intentional KB content in files named `._*` or `.DS_Store`.

## Docker containers

Remote KBs **cannot** be mounted when running AKB inside Docker on macOS. This is a Docker Desktop limitation, not an rclone or AKB limitation.

### Why it fails

Docker Desktop shares macOS paths into the Docker VM via **VirtioFS**. VirtioFS-backed bind mounts are always marked `private` propagation by the kernel, meaning submounts created inside the container are never reflected back on the host.

**FUSE** — requesting shared propagation is outright rejected:

```
docker: Error response from daemon: path /host_mnt/Users/... is mounted on
/host_mnt/Users but it is not a shared mount
```

**NFS** — `rclone nfsmount` succeeds inside the container (the `--privileged` flag grants the necessary capabilities), but the resulting mount lives in the container's mount namespace. The host sees only the empty underlying directory through the bind mount — not the NFS overlay.

### What this means

The `bin/akb-docker-s3.sh` and `bin/akb-docker-local.sh` wrappers are limited to **local KBs** (no `rclone_remote`) on macOS. For remote-backed KBs, use the native binary (`bin/akb.sh`) directly — it runs rclone on the host where mount propagation is not an issue.

On Linux Docker hosts, FUSE and NFS mounts inside the container work normally provided the appropriate kernel capabilities are available.
