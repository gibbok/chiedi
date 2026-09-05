//go:build unix

package source

import "syscall"

const nonblock = syscall.O_NONBLOCK
