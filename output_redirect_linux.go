package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/oraoto/go-pidfd"
)

func implementOutputFdRedirect() {
	outFdPidStr := os.Getenv("RUNNER_OUTFD_PID")
	if outFdPidStr == "" {
		return
	}

	outFdPid, err := strconv.Atoi(outFdPidStr)
	if err != nil {
		log.Fatalf("Invalid RUNNER_OUTFD_PID (must be an integer): '%s'", err)
	}
	outFdProc, err := pidfd.Open(outFdPid, 0)
	if err != nil {
		log.Fatalf("pidfd.Open(%d) failed: %s", outFdPid, err)
	}

	outFdStdOutStr := os.Getenv("RUNNER_OUTFD_STDOUT")
	if outFdStdOutStr != "" {
		outFdStdOut, err := strconv.Atoi(outFdStdOutStr)
		if err != nil {
			log.Fatalf("Invalid RUNNER_OUTFD_STDOUT (must be an integer): '%s'", err)
		}

		myStdoutFd, err := outFdProc.GetFd(outFdStdOut, 0)
		if err != nil {
			log.Fatalf("pidfd.GetFd(%d) failed: %s", outFdStdOut, err)
		}

		os.Stdout = os.NewFile(uintptr(myStdoutFd), fmt.Sprintf("/proc/%d/fd/%d", outFdPid, outFdStdOut))
	}

	outFdStdErrStr := os.Getenv("RUNNER_OUTFD_STDERR")
	if outFdStdErrStr != "" {
		outFdStdErr, err := strconv.Atoi(outFdStdErrStr)
		if err != nil {
			log.Fatalf("Invalid RUNNER_OUTFD_STDERR (must be an integer): '%s'", err)
		}

		myStderrFd, err := outFdProc.GetFd(outFdStdErr, 0)
		if err != nil {
			log.Fatalf("pidfd.GetFd(%d) failed: %s", outFdStdErr, err)
		}

		os.Stderr = os.NewFile(uintptr(myStderrFd), fmt.Sprintf("/proc/%d/fd/%d", outFdPid, outFdStdErr))
		log.Default().SetOutput(os.Stderr)
	}
}
