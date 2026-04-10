// Package mount manages the lifecycle of rclone FUSE/NFS mounts and plain
// local directory registrations used as knowledge base backing stores.
//
// The central type is Manager, which tracks active mounts and exposes Add,
// Unmount, Deregister, IsMounted, and Preflight. Manager is threaded through
// context.Context using IntoContext and FromContext.
//
// Remote KBs are mounted by spawning rclone as a child process (mount or
// nfsmount subcommand). The mount method is selected automatically based on
// FUSE availability unless overridden by the KB config.
package mount
