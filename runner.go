package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var version = "<dev>"

// Environment variables supported by runner:
const (
	MailToEnvVar      = "RUNNER_MAILTO"
	MailFromEnvVar    = "RUNNER_MAIL_FROM"
	SMTPUserEnvVar    = "RUNNER_SMTP_USER"
	SMTPPassEnvVar    = "RUNNER_SMTP_PASS"
	SMTPHostEnvVar    = "RUNNER_SMTP_HOST"
	SMTPPortEnvVar    = "RUNNER_SMTP_PORT"
	MailTabCharEnvVar = "RUNNER_MAIL_TAB_CHAR"

	OutFdPidEnvVar    = "RUNNER_OUTFD_PID"
	OutFdStdoutEnvVar = "RUNNER_OUTFD_STDOUT"
	OutFdStderrEnvVar = "RUNNER_OUTFD_STDERR"

	LogDirEnvVar = "RUNNER_LOG_DIR"

	HideEnvVarsEnvVar   = "RUNNER_HIDE_ENV"
	CensorEnvVarsEnvVar = "RUNNER_CENSOR_ENV"
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] -- /path/to/program --program-args\n", filepath.Base(os.Args[0]))
	_, _ = fmt.Fprintf(os.Stderr, "Run the given program, only printing its output if the program exits with an error, "+
		"or if the output contains (or does not contain) certain substrings.\n")
	_, _ = fmt.Fprintf(os.Stderr, "\nOptionally, all output is logged to a user-configurable directory.\n")
	_, _ = fmt.Fprintf(os.Stderr, "\nIf run as root or with CAP_SETUID and CAP_SETGID, the program can be run as a different user.\n")
	_, _ = fmt.Fprintf(os.Stderr, "\nLinux 5.6+ only: If run with CAP_SYS_PTRACE and the environment variables (%s and one or both of RUNNER_OUTFD_STD[OUT|ERR]), "+
		"all output will be redirected to those file descriptors on RUNNER_OUTFD_PID. This is useful in some"+
		"containerization situations. The container must be run with --cap-add CAP_SYS_PTRACE.\n", OutFdPidEnvVar)
	_, _ = fmt.Fprintf(os.Stderr, "\nOptions:\n")
	flag.PrintDefaults()
	_, _ = fmt.Fprintf(os.Stderr, "\nEnvironment variable-only options:\n")
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n    \tColon-separated list of environment variables whose values will be censored in output."+
		"\n    \tRUNNER_SMTP_PASS is always censored.\n", CensorEnvVarsEnvVar)
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n    \tColon-separated list of environment variables which will be entirely omitted from output.\n", HideEnvVarsEnvVar)
	_, _ = fmt.Fprintf(os.Stderr, "\nVersion:\n  runner %s\n", version)
	_, _ = fmt.Fprintf(os.Stderr, "\nGitHub:\n  https://github.com/cdzombak/runner\n")
	_, _ = fmt.Fprintf(os.Stderr, "\nAuthor:\n  Chris Dzombak <https://www.dzombak.com>\n")
}

func main() {
	implementOutputFdRedirect()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "<unknown hostname>"
		log.Printf("Failed to get hostname: %s", err)
	}

	var healthyExitCodes IntSlice
	flag.Var(&healthyExitCodes, "healthy-exit", "\"Healthy\" or \"success\" exit codes. "+
		"May be specified multiple times to provide more than one success exit code. (default: 0)")
	var printIfMatch StringSlice
	var printIfNotMatch StringSlice
	flag.Var(&printIfMatch, "print-if-match", "Print/mail output if the given (case-sensitive) string appears in the program's output, even if it was a healthy exit. "+
		"May be specified multiple times.")
	flag.Var(&printIfNotMatch, "print-if-not-match", "Print/mail output if the given (case-sensitive) string does not appear in the program's output, even if it was a healthy exit. "+
		"May be specified multiple times.")
	alwaysPrint := flag.Bool("always-print", false, "Always print/mail the program's output, sidestepping exit code and -print-if[-not]-match checks.")
	jobName := flag.String("job-name", "", "Job name used in failure notifications and log file name. (default: program name, without path)")
	hideEnv := flag.Bool("hide-env", false, "Hide the process's environment, which is normally printed & logged as part of the output.")
	logDir := flag.String("log-dir", "", "The directory to write run logs to. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", LogDirEnvVar))
	workDir := flag.String("work-dir", "", "Set the working directory for the program.")
	retries := flag.Int("retries", 0, "If the command fails, retry it this many times.")
	retryDelayInt := flag.Int("retry-delay", 0, "If the command fails, wait this many seconds before retrying.")
	asUser := flag.String("user", "", "Run the program as the given user. Ignored on Windows. "+
		"(If provided, runner must be run as root or with CAP_SETUID and CAP_SETGID.)")
	asUID := flag.Int("uid", -1, "Run the program as the given UID. Ignored on Windows. "+
		"(If provided, runner must be run as root or with CAP_SETUID.)")
	asGID := flag.Int("gid", -1, "Run the program as the given GID. Ignored on Windows. "+
		"(If provided, runner must be run as root or with CAP_SETGID.)")
	mailTo := flag.String("mailto", "", "Send an email to the given address if the program fails or its output would otherwise be printed per -healthy-exit/-print-if-[not]-match/-always-print. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", MailToEnvVar))
	mailFrom := flag.String("mail-from", "", "The email address to use as the From: address in failure emails. (default: runner@hostname) "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", MailFromEnvVar))
	smtpUser := flag.String("smtp-user", "", "Username for SMTP authentication. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", SMTPUserEnvVar))
	smtpPass := flag.String("smtp-pass", "", "Password for SMTP authentication. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", SMTPPassEnvVar))
	smtpHost := flag.String("smtp-host", "", "SMTP server hostname. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", SMTPHostEnvVar))
	smtpPort := flag.Int("smtp-port", 0, "SMTP server port. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable. (default: 25)", SMTPPortEnvVar))
	mailTabCharReplacement := flag.String("mail-tab-char", "", "Replace tab characters in emailed output by this string. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", MailTabCharEnvVar))
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

	var warningLogs []string

	var sysProcAttr *syscall.SysProcAttr
	var userHome string
	//goland:noinspection GoBoolExpressions
	if runtime.GOOS != "windows" {
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
		if *asUID != -1 || *asGID != -1 {
			sysProcAttr = &syscall.SysProcAttr{}
			sysProcAttr.Credential = &syscall.Credential{}
			if *asUID != -1 {
				sysProcAttr.Credential.Uid = uint32(*asUID)
			}
			if *asGID != -1 {
				sysProcAttr.Credential.Gid = uint32(*asGID)
			}

			u, err := user.LookupId(strconv.Itoa(*asUID))
			if err != nil && u != nil {
				userHome = u.HomeDir
			} else if err != nil {
				warningLogs = append(warningLogs, fmt.Sprintf("cannot find homedir for UID %d (%s); HOME will not be changed", *asUID, err))
			} else {
				warningLogs = append(warningLogs, fmt.Sprintf("cannot find homedir for UID %d; HOME will not be changed", *asUID))
			}
		}
	}

	mailOutput := false
	if *mailTo == "" {
		*mailTo = os.Getenv(MailToEnvVar)
	}
	if *mailFrom == "" {
		*mailFrom = os.Getenv(MailFromEnvVar)
	}
	if *mailFrom == "" {
		*mailFrom = "runner@" + hostname
	}
	if *smtpUser == "" {
		*smtpUser = os.Getenv(SMTPUserEnvVar)
	}
	if *smtpPass == "" {
		*smtpPass = os.Getenv(SMTPPassEnvVar)
	}
	if *smtpHost == "" {
		*smtpHost = os.Getenv(SMTPHostEnvVar)
	}
	if *smtpPort == 0 {
		smtpPortStr := os.Getenv(SMTPPortEnvVar)
		if smtpPortStr != "" {
			*smtpPort, err = strconv.Atoi(smtpPortStr)
			if err != nil {
				log.Fatalf("Failed to parse %s ('%s') as integer: %s", SMTPPortEnvVar, smtpPortStr, err)
			}
		}
	}
	if *smtpPort == 0 {
		*smtpPort = 25
	}
	if *mailTo != "" && strings.Contains(*mailTo, "@") {
		if *smtpUser != "" || *smtpPass != "" || *smtpHost != "" {
			mailOutput = true
		} else {
			log.Fatalf("If using -mailto (or the %s env var), you must also specify -smtp-user (%s), -smtp-pass (%s), -smtp-host (%s).",
				MailToEnvVar, SMTPUserEnvVar, SMTPPassEnvVar, SMTPHostEnvVar)
		}
	}
	if *mailTabCharReplacement == "" {
		*mailTabCharReplacement = os.Getenv(MailTabCharEnvVar)
	}

	triesRemaining := 1 + *retries
	programOutput := ""
	var startTime, endTime time.Time

	statusStr := "Failed"
	shouldPrint := true
	exitCode := -1

	for triesRemaining > 0 {
		if *retries > 0 && triesRemaining != 1+*retries {
			if *retryDelayInt > 0 {
				time.Sleep(time.Duration(*retryDelayInt) * time.Second)
			}
			programOutput = programOutput + fmt.Sprintf("\n- Retrying after %d seconds -\n\n", *retryDelayInt)
		}
		triesRemaining--

		startTime = time.Now()
		cmd := exec.Command(programName, programArgs...)
		if sysProcAttr != nil {
			cmd.SysProcAttr = sysProcAttr
		}
		cmd.Dir = *workDir
		cmd.Env = os.Environ()
		if userHome != "" {
			for i, v := range cmd.Env {
				if strings.HasPrefix(v, "HOME=") {
					cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
					break
				}
			}
			cmd.Env = append(cmd.Env, "HOME="+userHome)
		}
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
				shouldPrint = *alwaysPrint
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
	if *asUser != "" || *asUID != -1 || *asGID != -1 {
		if *asUser != "" {
			output = output + fmt.Sprintf("Run as user %s:\n", *asUser)
		} else {
			output = output + "Run as:\n"
		}
		output = output + fmt.Sprintf("\tUID: %d\n", *asUID)
		output = output + fmt.Sprintf("\tGID: %d\n\n", *asGID)
	}
	if !*hideEnv {
		output = output + "Environment:\n"
		for _, envVar := range os.Environ() {
			envVarPair := strings.SplitN(envVar, "=", 2)
			envVarName := envVarPair[0]
			if shouldHideEnvVar(envVarName) {
				continue
			}
			output = output + fmt.Sprintf("\t%s=%s\n", envVarName, censoredEnvVarValue(envVarName, envVarPair[1]))
		}
		output = output + "\n"
	}
	if len(warningLogs) > 0 {
		output = output + "--- Warnings ---\n\n"
		for _, warningLog := range warningLogs {
			output = output + fmt.Sprintf("%s\n", warningLog)
		}
		output = output + "\n"
	}
	output = output + "--- Program output ---\n\n"
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
				warningLogs = append(warningLogs, fmt.Sprintf("Failed to send email to %s: %s", *mailTo, err))
			}
		}
	}

	if *logDir == "" {
		*logDir = os.Getenv(LogDirEnvVar)
	}
	if *logDir != "" {
		if _, err := os.Stat(*logDir); os.IsNotExist(err) {
			err = os.MkdirAll(*logDir, 0770)
			if err != nil {
				log.Fatalf("Failed to create log directory '%s': %s", *logDir, err)
			}
			if *asUID != -1 || *asGID != -1 {
				err = os.Chown(*logDir, *asUID, *asGID)
				if err != nil {
					log.Fatalf("Failed to chown log directory '%s' (%d, %d): %s", *logDir, *asUID, *asGID, err)
				}
			}
		}

		logFileName := fmt.Sprintf("%s.%s.log",
			removeBadFilenameChars(*jobName),
			startTime.Format("2006-01-02T15-04-05.000-0700"),
		)
		logFile := filepath.Join(*logDir, logFileName)
		err = writeLogFile(logFile, output)
		if err != nil {
			log.Fatalf("Failed to write to log file '%s': %s", logFile, err)
		}
		if *asUID != -1 || *asGID != -1 {
			err = os.Chown(logFile, *asUID, *asGID)
			if err != nil {
				log.Fatalf("Failed to chown log file '%s' (%d, %d): %s", logFile, *asUID, *asGID, err)
			}
		}
	}
}
