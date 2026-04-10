// Package config defines the configuration model and storage interface for AKB.
//
// The central type is Interface, which backends implement to persist and retrieve
// the Config struct (a map of named knowledge bases). Two adapters are provided:
// adapter/localfs for single-process local use, and adapter/s3 for shared
// multi-process use with optimistic concurrency control.
//
// Config and the active Interface implementation are threaded through
// context.Context using IntoContext and FromContext.
package config
