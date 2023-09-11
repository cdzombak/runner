package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var version = "<dev>"

func usage() {
	fmt.Printf("Usage: %s [OPTIONS] -- /path/to/program --program-args\n", filepath.Base(os.Args[0]))
	fmt.Printf("Run the given program, only printing output to stdout/stderr if the program exits with an error.\n")
	fmt.Printf("Optionally, all output is logged to a user-configurable directory.\n")
	fmt.Printf("If run as root or with CAP_SETUID and CAP_SETGID, the program can be run as a different user.\n")
	fmt.Printf("\nOptions:\n")
	flag.PrintDefaults()
	fmt.Printf("\nVersion:\n  runner version %s\n", version)
	fmt.Printf("\nIssues:\n  https://github.com/cdzombak/runner/issues/new\n")
	fmt.Printf("\nAuthor: Chris Dzombak <https://www.dzombak.com>\n")
}

func main() {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}

	var healthyExitCodes IntSlice
	flag.Var(&healthyExitCodes, "healthy-exit", "\"Healthy\" or \"success\" exit codes. May be specified multiple times to provide more than one success exit code. (default: 0)")
	var printIfMatch StringSlice
	var printIfNotMatch StringSlice
	flag.Var(&printIfMatch, "print-if-match", "Print/mail output if the given (case-sensitive) string appears in the program's output, even if it was a healthy exit. May be specified multiple times.")
	flag.Var(&printIfNotMatch, "print-if-not-match", "Print/mail output if the given (case-sensitive) string does not appear in the program's output, even if it was a healthy exit. May be specified multiple times.")
	jobName := flag.String("job-name", "", "Job name used in failure notifications and log file name. (default: program name, without path)")
	hideEnv := flag.Bool("hide-env", false, "Hide the process's environment, which is normally printed & logged as part of the output.")
	logDir := flag.String("log-dir", "", "The directory to write run logs to. Can also be set by the RUNNER_LOG_DIR environment variable; this flag overrides the environment variable.")
	workDir := flag.String("work-dir", "", "Set the working directory for the program.")
	retries := flag.Int("retries", 0, "If the command fails, retry it this many times.")
	asUser := flag.String("user", "", "Run the program as the given user. (If provided, runner must be run as root or with CAP_SETUID and CAP_SETGID.)")
	asUID := flag.Int("uid", -1, "Run the program as the given UID. (If provided, runner must be run as root or with CAP_SETUID.)")
	asGID := flag.Int("gid", -1, "Run the program as the given GID. (If provided, runner must be run as root or with CAP_SETUID.)")
	mailTo := flag.String("mailto", "", "Send an email to the given address if the program fails or its output would otherwise be printer pet -print-if-[not]-match. Can also be set by the MAILTO environment variable; this flag overrides the environment variable.")
	mailFrom := flag.String("mail-from", "runner@"+hostname, "The email address to use as the From: address in failure emails.")
	smtpUser := flag.String("smtp-user", "", "Username for SMTP authentication.")
	smtpPass := flag.String("smtp-pass", "", "Password for SMTP authentication.")
	smtpHost := flag.String("smtp-host", "", "SMTP server hostname.")
	smtpPort := flag.Int("smtp-port", 25, "SMTP server port.")
	mailTabCharReplacement := flag.String("mail-tab-char", "", "Replace tab characters in emailed output by this string. (Can also be set by the RUNNER_MAIL_TAB_CHAR environment variable; this flag overrides the environment variable.)")
	printVersion := flag.Bool("version", false, "Print version and exit.")
	flag.Usage = usage
	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		os.Exit(0)
	}

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

	if *asUser != "" && (*asUID != -1 || *asGID != -1) {
		log.Fatalf("Cannot specify both -user and -uid/-gid")
	}
	if *asUser != "" {
		u, err := user.Lookup(*asUser)
		if err != nil {
			log.Fatalf("Failed to lookup user %s: %s", *asUser, err)
		}
		uid, err := strconv.ParseInt(u.Uid, 10, 32)
		if err != nil {
			log.Fatalf("Failed to parse UID %s as integer: %s", u.Uid, err)
		}
		gid, err := strconv.ParseInt(u.Gid, 10, 32)
		if err != nil {
			log.Fatalf("Failed to parse UID %s as integer: %s", u.Uid, err)
		}
		*asUID = int(uid)
		*asGID = int(gid)
	}
	var sysProcAttr *syscall.SysProcAttr = nil
	if *asUID != -1 || *asGID != -1 {
		sysProcAttr = &syscall.SysProcAttr{}
		sysProcAttr.Credential = &syscall.Credential{}
		if *asUID != -1 {
			sysProcAttr.Credential.Uid = uint32(*asUID)
		}
		if *asGID != -1 {
			sysProcAttr.Credential.Gid = uint32(*asGID)
		}
		log.Printf("Program will run as UID %d, GID %d", sysProcAttr.Credential.Uid, sysProcAttr.Credential.Gid)
	}

	mailOutput := false
	if *mailTo == "" {
		*mailTo = os.Getenv("MAILTO")
	}
	if *mailTo != "" && strings.Contains(*mailTo, "@") {
		if *smtpUser != "" || *smtpPass != "" || *smtpHost != "" {
			mailOutput = true
		} else {
			log.Println("If using -mailto (or the MAILTO env var), you must also specify -smtp-user, -smtp-pass, and -smtp-host.")
		}
	}
	if *mailTabCharReplacement == "" {
		*mailTabCharReplacement = os.Getenv("RUNNER_MAIL_TAB_CHAR")
	}

	triesRemaining := 1 + *retries
	programOutput := ""
	var startTime, endTime time.Time

	statusStr := "Failed"
	shouldPrint := true
	exitCode := -1

	for triesRemaining > 0 {
		if *retries > 0 && triesRemaining != 1+*retries {
			programOutput = programOutput + "\n- Retrying -\n\n"
		}
		triesRemaining--

		startTime = time.Now()
		cmd := exec.Command(programName, programArgs...)
		if sysProcAttr != nil {
			cmd.SysProcAttr = sysProcAttr
		}
		cmd.Dir = *workDir
		cmdOut, err := cmd.CombinedOutput()
		endTime = time.Now()

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

		exitCode = cmd.ProcessState.ExitCode()
		cmdOutStr := string(cmdOut)
		programOutput = programOutput + cmdOutStr

		for _, v := range healthyExitCodes {
			if exitCode == v {
				statusStr = "Succeeded"
				shouldPrint = false
				triesRemaining = 0
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
	}

	duration := endTime.Sub(startTime)

	if *workDir == "" {
		var err error
		*workDir, err = os.Getwd()
		if err != nil {
			panic("failed to get working directory")
		}
	}

	output := fmt.Sprintf("[%s] %s running %s\nWorking directory: %s\nCommand: %s\nExit code: %d\n\nDuration: %s\nStart time: %s\nEnd time: %s\nRetries allowed: %d\n\n",
		hostname,
		statusStr,
		*jobName,
		*workDir,
		exec.Command(programName, programArgs...).String(),
		exitCode,
		duration.String(),
		startTime.Format("2006-01-02 15:04:05.000 -0700"),
		endTime.Format("2006-01-02 15:04:05.000 -0700"),
		*retries,
	)
	if !*hideEnv {
		output = output + "Environment:\n"
		for _, v := range os.Environ() {
			output = output + fmt.Sprintf("\t%s\n", v)
		}
		output = output + "\n"
	}
	output = output + "--- Program output follows: ---\n\n"
	if len(programOutput) == 0 {
		output = output + "(no output produced)\n"
	} else {
		output = output + programOutput + "\n"
	}

	if shouldPrint {
		fmt.Printf(output)

		if mailOutput {
			body := strings.ReplaceAll(output, "\n", "\r\n")
			if *mailTabCharReplacement != "" {
				body = strings.ReplaceAll(body, "\t", *mailTabCharReplacement)
			}

			msg := []byte(fmt.Sprintf(
				"From: %s\r\n"+
					"To: %s\r\n"+
					"Subject: %s\r\n"+
					"X-Mailer: %s\r\n\r\n"+
					"%s\r\n",
				*mailFrom, *mailTo,
				fmt.Sprintf("[%s] %s running %s", hostname, statusStr, *jobName),
				"runner "+version,
				body,
			))
			smtpAddr := fmt.Sprintf("%s:%d", *smtpHost, *smtpPort)
			auth := smtp.PlainAuth("", *smtpUser, *smtpPass, *smtpHost)
			err = smtp.SendMail(smtpAddr, auth, *mailFrom, []string{*mailTo}, msg)

			if err != nil {
				log.Printf("Failed to send email to %s: %s", *mailTo, err)
			}
		}
	}

	if *logDir == "" {
		*logDir = os.Getenv("RUNNER_LOG_DIR")
	}
	if *logDir != "" {
		err := os.MkdirAll(*logDir, os.ModePerm)
		if err != nil {
			log.Fatalf("Failed to create log directory %s: %s", *logDir, err)
		}

		logFileName := fmt.Sprintf("%s.%s.log",
			removeBadFilenameChars(*jobName),
			startTime.Format("2006-01-02T15-04-05.000-0700"),
		)
		logFile := filepath.Join(*logDir, logFileName)
		err = writeToFile(logFile, output)
		if err != nil {
			log.Fatalf("Failed to write to log file %s: %s", logFile, err)
		}
	}
}
