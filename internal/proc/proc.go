//go:build darwin

// Package proc reads what reopen needs to know of processes: a process's
// arguments and start time, from the kernel, and every process's command
// line, from ps, macOS's own. It only reads.
package proc

import (
	"bytes"
	"context"
	"encoding/binary"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Info is a live process: when it started and the arguments it started
// with, Args[0] being the name it was started by.
type Info struct {
	PID   int
	Start time.Time
	Args  []string
}

// Get returns the process pid, or false when none runs or it can't be read:
// a zombie, a process of another user, or a kernel that answers in a way
// this code doesn't know.
//
// The arguments come from the kernel (KERN_PROCARGS2) rather than ps, which
// joins them with spaces: a first prompt such as "use
// --dangerously-skip-permissions" would look like the flag, and a folder
// with a space in its name like two arguments.
func Get(pid int) (Info, bool) {
	if pid <= 0 {
		return Info{}, false
	}
	start, ok := startTime(pid)
	if !ok {
		return Info{}, false
	}
	args, ok := arguments(pid)
	if !ok || len(args) == 0 {
		return Info{}, false
	}
	return Info{PID: pid, Start: start, Args: args}, true
}

// sysctl calls sysctl(3) with mib, filling buf; it returns how many bytes
// the kernel wrote. syscall has no exported call that takes a numeric mib.
func sysctl(mib []int32, buf []byte) (int, error) {
	n := uintptr(len(buf))
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

const (
	ctlKern       = 1
	kernProc      = 14
	kernProcPID   = 1
	kernProcargs2 = 49
	// kinfoProcSize is sizeof(struct kinfo_proc) on 64-bit macOS.
	kinfoProcSize = 648
	// In its first member, struct extern_proc: p_starttime (a timeval) at 0,
	// p_stat at 36, p_pid at 40.
	offStat = 36
	offPID  = 40
	sZomb   = 5 // p_stat of a zombie
)

// startTime is when pid started, to the microsecond. The pid the kernel
// writes back is checked against pid, so a layout this code doesn't know
// gives no answer rather than a wrong one.
func startTime(pid int) (time.Time, bool) {
	buf := make([]byte, kinfoProcSize)
	n, err := sysctl([]int32{ctlKern, kernProc, kernProcPID, int32(pid)}, buf)
	if err != nil || n != kinfoProcSize {
		return time.Time{}, false // n is 0 when no such process runs
	}
	if int32(binary.LittleEndian.Uint32(buf[offPID:])) != int32(pid) || buf[offStat] == sZomb {
		return time.Time{}, false
	}
	sec := int64(binary.LittleEndian.Uint64(buf[0:]))
	usec := int64(int32(binary.LittleEndian.Uint32(buf[8:])))
	return time.Unix(sec, usec*1000), true
}

// arguments is pid's argv. KERN_PROCARGS2 gives argc, then the executable's
// path, then NULs to align, then argc strings, then the environment, which
// is never read.
func arguments(pid int) ([]string, bool) {
	size, err := syscall.SysctlUint32("kern.argmax")
	if err != nil || size == 0 {
		size = 1 << 20
	}
	buf := make([]byte, size)
	n, err := sysctl([]int32{ctlKern, kernProcargs2, int32(pid)}, buf)
	if err != nil || n < 4 {
		return nil, false
	}
	buf = buf[:n]
	argc := int(int32(binary.LittleEndian.Uint32(buf)))
	rest := buf[4:]
	i := bytes.IndexByte(rest, 0) // the end of the executable's path
	if i < 0 || argc <= 0 {
		return nil, false
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	args := make([]string, 0, argc)
	for len(args) < argc {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			return nil, false
		}
		args = append(args, string(rest[:j]))
		rest = rest[j+1:]
	}
	return args, true
}

// Line is one process as ps lists it: its pid, and its command line split
// at spaces. That loses the bounds of an argument with a space in it, but a
// session id has none, which is all Scan is for.
type Line struct {
	PID    int
	Fields []string
}

// Scan lists every process the operator can see, with ps. It waits five
// seconds at most.
func Scan() ([]Line, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axww", "-o", "pid=,args=")
	cmd.Env = []string{"LC_ALL=C"}
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []Line
	for l := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		lines = append(lines, Line{PID: pid, Fields: f[1:]})
	}
	return lines, nil
}

// Named says whether args start a program called name: args[0] is name or
// a path to it, or it is node running a script called name, as an npm
// install of a CLI can be.
func Named(args []string, name string) bool {
	if len(args) == 0 {
		return false
	}
	if filepath.Base(args[0]) == name {
		return true
	}
	return filepath.Base(args[0]) == "node" && len(args) > 1 && filepath.Base(args[1]) == name
}

// OpenFiles lists the files the processes pids hold open, with lsof,
// macOS's own. It waits five seconds at most.
func OpenFiles(pids []int) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ps := make([]string, len(pids))
	for i, p := range pids {
		ps[i] = strconv.Itoa(p)
	}
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-p", strings.Join(ps, ","), "-Fn")
	cmd.Env = []string{"LC_ALL=C"}
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var files []string
	for l := range strings.SplitSeq(string(out), "\n") {
		if f, ok := strings.CutPrefix(l, "n"); ok {
			files = append(files, f)
		}
	}
	return files, nil
}
