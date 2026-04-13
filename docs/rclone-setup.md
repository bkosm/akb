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

**Recommended:** download the official binary from [rclone.org/downloads](https://rclone.org/downloads/):

```bash
mkdir -p ~/.bin

# macOS (Apple Silicon)
curl -O https://downloads.rclone.org/rclone-current-osx-arm64.zip
unzip rclone-current-osx-arm64.zip
cp rclone-*-osx-arm64/rclone ~/.bin/
chmod +x ~/.bin/rclone

# macOS (Intel)
curl -O https://downloads.rclone.org/rclone-current-osx-amd64.zip
unzip rclone-current-osx-amd64.zip
cp rclone-*-osx-amd64/rclone ~/.bin/
chmod +x ~/.bin/rclone

# Linux
curl -O https://downloads.rclone.org/rclone-current-linux-amd64.zip
unzip rclone-current-linux-amd64.zip
cp rclone-*-linux-amd64/rclone ~/.bin/
chmod +x ~/.bin/rclone
```

Make sure `~/.bin` is on your `PATH` (add to `~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$HOME/.bin:$PATH"
```

If you already have the Homebrew version installed, the official binary in
`~/.bin` will take precedence as long as `~/.bin` appears before
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

The `bin/akb-docker-s3.sh` and `bin/akb-docker-local.sh` wrappers are limited to **local KBs** (no `rclone_remote`) on macOS. For remote-backed KBs, use the native binary (`bin/stdio.sh`) directly — it runs rclone on the host where mount propagation is not an issue.

On Linux Docker hosts, FUSE and NFS mounts inside the container work normally provided the appropriate kernel capabilities are available.
