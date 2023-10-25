package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var version = "<dev>"

// Environment variables supporting email delivery:
const (
	MailToEnvVar      = "RUNNER_MAILTO"
	MailFromEnvVar    = "RUNNER_MAIL_FROM"
	SMTPUserEnvVar    = "RUNNER_SMTP_USER"
	SMTPPassEnvVar    = "RUNNER_SMTP_PASS"
	SMTPHostEnvVar    = "RUNNER_SMTP_HOST"
	SMTPPortEnvVar    = "RUNNER_SMTP_PORT"
	MailTabCharEnvVar = "RUNNER_MAIL_TAB_CHAR"
)

// Environment variables supporting ntfy delivery:
const (
	NtfyServerEnvVar      = "RUNNER_NTFY_SERVER"
	NtfyTopicEnvVar       = "RUNNER_NTFY_TOPIC"
	NtfyTagsEnvVar        = "RUNNER_NTFY_TAGS"
	NtfyPriorityEnvVar    = "RUNNER_NTFY_PRIORITY"
	NtfyEmailEnvVar       = "RUNNER_NTFY_EMAIL"
	NtfyAccessTokenEnvVar = "RUNNER_NTFY_ACCESS_TOKEN"
)

// Environment variables supporting Discord delivery:
const (
	DiscordWebhookEnvVar = "RUNNER_DISCORD_WEBHOOK"
)

// Environment variables supporting output redirection:
const (
	OutFdPidEnvVar    = "RUNNER_OUTFD_PID"
	OutFdStdoutEnvVar = "RUNNER_OUTFD_STDOUT"
	OutFdStderrEnvVar = "RUNNER_OUTFD_STDERR"
)

// Environment variables controlling output:
const (
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
		"\n    \tRUNNER_SMTP_PASS and RUNNER_NTFY_ACCESS_TOKEN are always censored.\n", CensorEnvVarsEnvVar)
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

	// job control flags:
	var healthyExitCodes IntSlice
	flag.Var(&healthyExitCodes, "healthy-exit", "\"Healthy\" or \"success\" exit codes. "+
		"May be specified multiple times to provide more than one success exit code. (default: 0)")
	retries := flag.Int("retries", 0, "If the command fails, retry it this many times.")
	retryDelayInt := flag.Int("retry-delay", 0, "If the command fails, wait this many seconds before retrying.")

	// output configuration flags:
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

	// run-as-user flags:
	asUser := flag.String("user", "", "Run the program as the given user. Ignored on Windows. "+
		"(If provided, runner must be run as root or with CAP_SETUID and CAP_SETGID.)")
	asUID := flag.Int("uid", -1, "Run the program as the given UID. Ignored on Windows. "+
		"(If provided, runner must be run as root or with CAP_SETUID.)")
	asGID := flag.Int("gid", -1, "Run the program as the given GID. Ignored on Windows. "+
		"(If provided, runner must be run as root or with CAP_SETGID.)")

	// mail delivery flags:
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
	smtpPort := flag.Int("smtp-port", 25, "SMTP server port. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", SMTPPortEnvVar))
	mailTabCharReplacement := flag.String("mail-tab-char", "", "Replace tab characters in emailed output by this string. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", MailTabCharEnvVar))

	// ntfy delivery flags:
	ntfyServer := flag.String("ntfy-server", "", "Send a notification to the given ntfy server if the program fails or its output would otherwise be printed per -healthy-exit/-print-if-[not]-match/-always-print. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", NtfyServerEnvVar))
	ntfyTopic := flag.String("ntfy-topic", "", "The ntfy topic to send to. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", NtfyTopicEnvVar))
	ntfyTags := flag.String("ntfy-tags", "", "Comma-separated list of ntfy tags to send. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", NtfyTagsEnvVar))
	ntfyPriority := flag.Int("ntfy-priority", 3, "Priority for the notification sent to ntfy. Must be between 1-5, inclusive. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", NtfyPriorityEnvVar))
	ntfyEmail := flag.String("ntfy-email", "", "If set, tell ntfy to send an email to this address. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", NtfyEmailEnvVar))
	ntfyAccessToken := flag.String("ntfy-access-token", "", "If set, use this access token for ntfy. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", NtfyAccessTokenEnvVar))

	// Discord delivery flags:
	discordHookURL := flag.String("discord-webhook", "", "If set, post to this Discord webhook if the program fails or its output would otherwise be printed per -healthy-exit/-print-if-[not]-match/-always-print. "+
		fmt.Sprintf("Can also be set by the %s environment variable; this flag overrides the environment variable.", DiscordWebhookEnvVar))

	printVersion := flag.Bool("version", false, "Print version and exit.")
	flag.Usage = usage
	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Configuration and validation:

	runCfg := &runConfig{
		programName:      flag.Arg(0),
		workDir:          *workDir,
		healthyExitCodes: healthyExitCodes,
		retries:          *retries,
		outputConfig: &runOutputConfig{
			jobName:         *jobName,
			hostname:        hostname,
			hideEnv:         *hideEnv,
			alwaysPrint:     *alwaysPrint,
			printIfMatch:    printIfMatch,
			printIfNotMatch: printIfNotMatch,
		},
		runAsUser: nil,
	}
	if runCfg.programName == "" {
		flag.Usage()
		os.Exit(1)
	}
	if len(flag.Args()) > 1 {
		runCfg.programArgs = flag.Args()[1:]
	}
	if runCfg.outputConfig.jobName == "" {
		runCfg.outputConfig.jobName = filepath.Base(runCfg.programName)
	}
	if len(runCfg.healthyExitCodes) == 0 {
		runCfg.healthyExitCodes = []int{0}
	}
	if *retryDelayInt > 0 {
		runCfg.retryDelay = time.Duration(*retryDelayInt) * time.Second
	}

	var runAsConfig *runAsUserConfig
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
			runAsConfig = &runAsUserConfig{
				runAsUID: *asUID,
				runAsGID: *asGID,
				sysProcAttr: &syscall.SysProcAttr{
					Credential: &syscall.Credential{},
				},
				runAsUserName: *asUser,
			}
			if *asUID != -1 {
				runAsConfig.sysProcAttr.Credential.Uid = uint32(*asUID)
			}
			if *asGID != -1 {
				runAsConfig.sysProcAttr.Credential.Gid = uint32(*asGID)
			}

			u, err := user.LookupId(strconv.Itoa(*asUID))
			if err != nil && u != nil {
				runAsConfig.userHome = u.HomeDir
			} else if err != nil {
				runCfg.outputConfig.addSetupWarning(fmt.Sprintf("cannot find homedir for UID %d (%s); HOME will not be changed", *asUID, err))
			} else {
				runCfg.outputConfig.addSetupWarning(fmt.Sprintf("cannot find homedir for UID %d; HOME will not be changed", *asUID))
			}
		}
	}
	if runAsConfig != nil {
		runCfg.runAsUser = runAsConfig
	}

	deliveryCfg := &deliveryConfig{}

	shouldMailOutput := false
	mailCfg := &mailDeliveryConfig{
		mailTo:             *mailTo,
		mailFrom:           *mailFrom,
		smtpUser:           *smtpUser,
		smtpPassword:       *smtpPass,
		smtpHost:           *smtpHost,
		smtpPort:           *smtpPort,
		tabCharReplacement: *mailTabCharReplacement,
	}
	if mailCfg.mailTo == "" {
		mailCfg.mailTo = os.Getenv(MailToEnvVar)
	}
	if mailCfg.mailFrom == "" {
		mailCfg.mailFrom = os.Getenv(MailFromEnvVar)
	}
	if mailCfg.mailFrom == "" {
		mailCfg.mailFrom = "runner@" + hostname
	}
	if mailCfg.smtpUser == "" {
		mailCfg.smtpUser = os.Getenv(SMTPUserEnvVar)
	}
	if mailCfg.smtpPassword == "" {
		mailCfg.smtpPassword = os.Getenv(SMTPPassEnvVar)
	}
	if mailCfg.smtpHost == "" {
		mailCfg.smtpHost = os.Getenv(SMTPHostEnvVar)
	}
	if mailCfg.tabCharReplacement == "" {
		mailCfg.tabCharReplacement = os.Getenv(MailTabCharEnvVar)
	}
	if os.Getenv(SMTPPortEnvVar) != "" && !WasFlagGiven("smtp-port") {
		smtpPortStr := os.Getenv(SMTPPortEnvVar)
		mailCfg.smtpPort, err = strconv.Atoi(smtpPortStr)
		if err != nil {
			log.Fatalf("Failed to parse %s ('%s') as integer: %s", SMTPPortEnvVar, smtpPortStr, err)
		}
	}
	if mailCfg.mailTo != "" && strings.Contains(mailCfg.mailTo, "@") {
		if *smtpUser != "" || *smtpPass != "" || *smtpHost != "" {
			shouldMailOutput = true

			if mailCfg.smtpPort < 1 || mailCfg.smtpPort > 65535 {
				runCfg.outputConfig.addSetupWarning(fmt.Sprintf(
					"Invalid SMTP port %d given; using default of 25 instead", mailCfg.smtpPort))
				mailCfg.smtpPort = 25
			}
		} else {
			runCfg.outputConfig.addSetupWarning(fmt.Sprintf(
				"If using -mailto (or the %s env var), you must also specify -smtp-user (%s), -smtp-pass (%s), -smtp-host (%s).",
				MailToEnvVar, SMTPUserEnvVar, SMTPPassEnvVar, SMTPHostEnvVar,
			))
		}
	}
	if shouldMailOutput {
		deliveryCfg.mail = mailCfg
	}

	shouldNtfyOutput := false
	ntfyCfg := &ntfyDeliveryConfig{
		ntfyTopic:       *ntfyTopic,
		ntfyTags:        *ntfyTags,
		ntfyEmail:       *ntfyEmail,
		ntfyAccessToken: *ntfyAccessToken,
		ntfyPriority:    *ntfyPriority,
	}
	if *ntfyServer == "" {
		*ntfyServer = os.Getenv(NtfyServerEnvVar)
	}
	if ntfyCfg.ntfyTopic == "" {
		ntfyCfg.ntfyTopic = os.Getenv(NtfyTopicEnvVar)
	}
	if ntfyCfg.ntfyTags == "" {
		ntfyCfg.ntfyTags = os.Getenv(NtfyTagsEnvVar)
	}
	if ntfyCfg.ntfyEmail == "" {
		ntfyCfg.ntfyEmail = os.Getenv(NtfyEmailEnvVar)
	}
	if ntfyCfg.ntfyAccessToken == "" {
		ntfyCfg.ntfyAccessToken = os.Getenv(NtfyAccessTokenEnvVar)
	}
	if os.Getenv(NtfyPriorityEnvVar) != "" && !WasFlagGiven("ntfy-priority") {
		ntfyPriorityStr := os.Getenv(NtfyPriorityEnvVar)
		ntfyCfg.ntfyPriority, err = strconv.Atoi(ntfyPriorityStr)
		if err != nil {
			log.Fatalf("Failed to parse the given %s ('%s') as integer: %s", NtfyPriorityEnvVar, ntfyPriorityStr, err)
		}
	}
	if *ntfyServer != "" {
		if !strings.HasPrefix(strings.ToLower(*ntfyServer), "http") {
			*ntfyServer = "https://" + *ntfyServer
		}
		ntfyCfg.ntfyServerURL, err = url.Parse(*ntfyServer)
		if err != nil {
			log.Fatalf("Failed to parse the given ntfy server URL ('%s'): %s", *ntfyServer, err)
		}
		if ntfyCfg.ntfyTopic != "" {
			shouldNtfyOutput = true
		} else {
			runCfg.outputConfig.addSetupWarning(fmt.Sprintf(
				"If using -ntfy-server (or the %s env var), you must also specify -ntfy-topic (%s).",
				NtfyServerEnvVar, NtfyTopicEnvVar,
			))
		}
	}
	if ntfyCfg.ntfyPriority < 1 || ntfyCfg.ntfyPriority > 5 {
		runCfg.outputConfig.addSetupWarning(fmt.Sprintf(
			"Invalid ntfy priority %d given; must be between 1-5, inclusive.", ntfyCfg.ntfyPriority))
		ntfyCfg.ntfyPriority = 3
	}
	if shouldNtfyOutput {
		deliveryCfg.ntfy = ntfyCfg
	}

	discordCfg := &discordDeliveryConfig{
		discordWebhookURL: *discordHookURL,
	}
	if discordCfg.discordWebhookURL == "" {
		discordCfg.discordWebhookURL = os.Getenv(DiscordWebhookEnvVar)
	}
	if discordCfg.discordWebhookURL != "" {
		if !strings.HasPrefix(strings.ToLower(discordCfg.discordWebhookURL), "http") {
			discordCfg.discordWebhookURL = "https://" + discordCfg.discordWebhookURL
		}
		deliveryCfg.discord = discordCfg
	}

	logCfg := &logConfig{
		logDir:   *logDir,
		runAsUID: -1,
		runAsGID: -1,
	}
	if logCfg.logDir == "" {
		logCfg.logDir = os.Getenv(LogDirEnvVar)
	}
	if runAsConfig != nil {
		logCfg.runAsUID = runAsConfig.runAsUID
		logCfg.runAsGID = runAsConfig.runAsGID
	}

	// Configuration is (finally) complete!
	// Run the program, print+deliver output if necessary, and write log file[s].

	runOut := runner(runCfg)

	logFileName := fmt.Sprintf("%s.%s.log",
		removeBadFilenameChars(runOut.jobName),
		runOut.startTime.Format("2006-01-02T15-04-05.000-0700"),
	)
	if deliveryCfg.discord != nil {
		deliveryCfg.discord.logFileName = logFileName
	}
	logCfg.logFileName = logFileName

	var deliveryErrs []error
	if runOut.shouldPrint {
		fmt.Print(runOut.output)
		deliveryErrs = executeDeliveries(deliveryCfg, runOut)
	}

	err = writeLogs(logCfg, runOut, deliveryErrs)
	if err != nil {
		log.Fatalf("Failed to write logs: %s", err)
	}
}

func productIdentifier() string {
	return fmt.Sprintf("runner / %s (https://github.com/cdzombak/runner)", version)
}
