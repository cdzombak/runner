package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/oraoto/go-pidfd"
)

func implementOutputFdRedirect() {
	outFdPidStr := os.Getenv(OutFdPidEnvVar)
	if outFdPidStr == "" {
		return
	}

	outFdPid, err := strconv.Atoi(outFdPidStr)
	if err != nil {
		log.Fatalf("Invalid %s (must be an integer): '%s'", OutFdPidEnvVar, err)
	}
	outFdProc, err := pidfd.Open(outFdPid, 0)
	if err != nil {
		log.Fatalf("pidfd.Open(%d) failed: %s", outFdPid, err)
	}

	outFdStdOutStr := os.Getenv(OutFdStdoutEnvVar)
	if outFdStdOutStr != "" {
		outFdStdOut, err := strconv.Atoi(outFdStdOutStr)
		if err != nil {
			log.Fatalf("Invalid %s (must be an integer): '%s'", OutFdStdoutEnvVar, err)
		}

		myStdoutFd, err := outFdProc.GetFd(outFdStdOut, 0)
		if err != nil {
			log.Fatalf("pidfd.GetFd(%d) failed: %s", outFdStdOut, err)
		}

		os.Stdout = os.NewFile(uintptr(myStdoutFd), fmt.Sprintf("/proc/%d/fd/%d", outFdPid, outFdStdOut))
	}

	outFdStdErrStr := os.Getenv(OutFdStderrEnvVar)
	if outFdStdErrStr != "" {
		outFdStdErr, err := strconv.Atoi(outFdStdErrStr)
		if err != nil {
			log.Fatalf("Invalid %s (must be an integer): '%s'", OutFdStderrEnvVar, err)
		}

		myStderrFd, err := outFdProc.GetFd(outFdStdErr, 0)
		if err != nil {
			log.Fatalf("pidfd.GetFd(%d) failed: %s", outFdStdErr, err)
		}

		os.Stderr = os.NewFile(uintptr(myStderrFd), fmt.Sprintf("/proc/%d/fd/%d", outFdPid, outFdStdErr))
		log.Default().SetOutput(os.Stderr)
	}
}
