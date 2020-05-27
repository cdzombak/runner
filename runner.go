package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Based on https://lawlessguy.wordpress.com/2013/07/23/filling-a-slice-using-command-line-flags-in-go-golang/ & https://stackoverflow.com/questions/28322997/how-to-get-a-list-of-values-into-a-flag-in-golang

type intslice []int

func (i *intslice) String() string {
	return fmt.Sprintf("%d", *i)
}

func (i *intslice) Set(value string) error {
	tmp, err := strconv.Atoi(value)
	if err != nil {
		*i = append(*i, -1)
	} else {
		*i = append(*i, tmp)
	}
	return nil
}

type strslice []string

func (i *strslice) String() string {
	return "my string representation"
}

func (i *strslice) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func usage() {
	fmt.Printf("Usage: %s [OPTIONS] -- /path/to/program --program-args\n", os.Args[0])
	fmt.Printf("Run the given program, only printing output to stdout/stderr if the program exits with an error.\n\n")
	fmt.Printf("Options:\n")
	flag.PrintDefaults()
	fmt.Printf("\nIssues:\n  https://github.com/cdzombak/runner/issues/new\n")
	fmt.Printf("\nAuthor: Chris Dzombak <https://www.dzombak.com>\n")
}

func main() {
	var healthyExitCodes intslice
	flag.Var(&healthyExitCodes, "healthy-exit", "\"Healthy\" or \"success\" exit codes. May be specified multiple times to provide more than one success exit code. (default: 0)")
	var printIfMatch strslice
	var printIfNotMatch strslice
	flag.Var(&printIfMatch, "print-if-match", "Print output if the given (case-sensitive) string appears in the program's output, even if it was a healthy exit. May be specified multiple times.")
	flag.Var(&printIfNotMatch, "print-if-not-match", "Print output if the given (case-sensitive) string does not appear in the program's output, even if it was a healthy exit. May be specified multiple times.")
	jobName := flag.String("job-name", "", "Job name used in failure notifications and log file name. (default: program name, without path)")
	hideEnv := flag.Bool("hide-env", false, "Hide the process's environment, which is normally printed & logged as part of the output.")
	logDir := flag.String("log-dir", "", "The directory to write run logs to. Can also be set by the RUNNER_LOG_DIR environment variable; this flag overrides the environment variable.")
	workDir := flag.String("work-dir", "", "Set the working directory for the program.")
	flag.Usage = usage
	flag.Parse()
	programName := flag.Arg(0)
	var programArgs []string
	if len(flag.Args()) > 1 {
		programArgs = flag.Args()[1:]
	}

	if programName == "" {
		flag.Usage()
		os.Exit(1)
	}
	if *jobName == "" {
		*jobName = filepath.Base(programName)
	}
	if len(healthyExitCodes) == 0 {
		healthyExitCodes = []int{0}
	}

	cmd := exec.Command(programName, programArgs...)
	cmd.Dir = *workDir
	startTime := time.Now()
	cmdOut, err := cmd.CombinedOutput()
	endTime := time.Now()

	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			err = nil
		} else {
			log.Fatalf("Failed to run %s: %s", cmd.String(), err)
		}
	}
	if cmd.ProcessState == nil {
		panic("cmd.ProcessState should not be nil after running")
	}

	exitCode := cmd.ProcessState.ExitCode()
	cmdOutStr := string(cmdOut)
	duration := endTime.Sub(startTime)

	statusStr := "Failure"
	shouldPrint := true
	for _, v := range healthyExitCodes {
		if exitCode == v {
			statusStr = "Success"
			shouldPrint = false
		}
	}
	if !shouldPrint {
		for _, v := range printIfMatch {
			if strings.Contains(cmdOutStr, v) {
				shouldPrint = true
				break
			}
		}
	}
	if !shouldPrint {
		for _, v := range printIfNotMatch {
			if !strings.Contains(cmdOutStr, v) {
				shouldPrint = true
				break
			}
		}
	}

	if *workDir == "" {
		*workDir, err = os.Getwd()
		if err != nil {
			panic("failed to get working directory")
		}
	}

	output := fmt.Sprintf("%s running %s\nCommand: %s\nExit code: %d\nWorking directory: %s\n\nDuration: %s\nStart time: %s\nEnd time: %s\n\n",
		statusStr,
		*jobName,
		cmd.String(),
		exitCode,
		*workDir,
		duration.String(),
		startTime.Format("2006-01-02 15:04:05.000 -0700"),
		endTime.Format("2006-01-02 15:04:05.000 -0700"),
	)
	if !*hideEnv {
		output = output + "Environment:\n"
		for _, v := range os.Environ() {
			output = output + fmt.Sprintf("\t%s\n", v)
		}
		output = output + "\n"
	}
	output = output + "--- Program output follows: ---\n\n"
	if len(cmdOut) == 0 {
		output = output + "(no output produced)\n"
	} else {
		output = output + cmdOutStr + "\n"
	}

	if shouldPrint {
		fmt.Printf(output)
	}

	if *logDir == "" {
		*logDir = os.Getenv("RUNNER_LOG_DIR")
	}
	if *logDir != "" {
		err = os.MkdirAll(*logDir, os.ModePerm)
		if err != nil {
			log.Fatalf("Failed to create log directory %s: %s", *logDir, err)
		}

		fname := fmt.Sprintf("%s.%s.log",
			removeBadFilenameChars(*jobName),
			startTime.Format("2006-01-02T15-04-05.000-0700"),
		)
		logfile := filepath.Join(*logDir, fname)
		err = writeToFile(logfile, output)
		if err != nil {
			log.Fatalf("Failed to write to log file %s: %s", logfile, err)
		}
	}
}

func writeToFile(filename string, data string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data)
	if err != nil {
		return err
	}
	return file.Sync()
}

func removeBadFilenameChars(filename string) string {
	badChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "'", "<", ">", ".", " "}
	for _, v := range badChars {
		filename = strings.ReplaceAll(filename, v, "-")
	}
	return filename
}
