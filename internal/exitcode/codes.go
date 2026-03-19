// Package exitcode defines process exit codes for CI integration.
package exitcode

const (
	OK              = 0
	ConfigError     = 1
	ConnectionError = 2
	DumpError       = 3
	PartitionError  = 4
	GitError        = 5
	RestoreError    = 6
	PartialFailure  = 7
)
