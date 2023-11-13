package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// runConfig determines how to run the program, check if it failed,
// retry it, and produce output.
// outputConfig must be non-nil; runAsUser may be nil.
type runConfig struct {
	programName      string
	programArgs      []string
	workDir          string
	healthyExitCodes IntSlice
	retries          int
	retryDelay       time.Duration
	outputConfig     *runOutputConfig
	runAsUser        *runAsUserConfig
}

type runOutputConfig struct {
	jobName         string
	hostname        string
	hideEnv         bool
	alwaysPrint     bool
	printIfMatch    StringSlice
	printIfNotMatch StringSlice
	setupWarnings   StringSlice
}

// runAsUserConfig, if non-nil, must be internally consistent (e.g. the sysProcAttr
// must match runAsUID and runAsGID), and all fields must be non-nil.
type runAsUserConfig struct {
	runAsUID      int
	runAsGID      int
	sysProcAttr   *syscall.SysProcAttr
	runAsUserName string
	userHome      string
}

type runOutput struct {
	output      string
	summaryLine string
	emoj        string
	jobName     string
	startTime   time.Time
	endTime     time.Time
	succeeded   bool
	shouldPrint bool
}

const (
	statusFailed    = "Failed"
	statusSucceeded = "Succeeded"
)

func runner(config *runConfig) *runOutput {
	programOutput := strings.Builder{}
	var startTime, endTime time.Time

	triesRemaining := 1 + config.retries
	succeeded := false
	shouldPrint := true
	exitCode := -1

	for triesRemaining > 0 {
		isRetry := config.retries > 0 && triesRemaining != 1+config.retries
		if isRetry {
			if config.retryDelay > 0 {
				time.Sleep(config.retryDelay)
			}
			programOutput.WriteString(fmt.Sprintf(
				"\n- Retrying after %.0f seconds -\n\n",
				config.retryDelay.Round(time.Second).Seconds(),
			))
		}
		triesRemaining--

		startTime = time.Now()
		cmd := exec.Command(config.programName, config.programArgs...)
		if config.runAsUser != nil {
			cmd.SysProcAttr = config.runAsUser.sysProcAttr
		}
		cmd.Dir = config.workDir
		cmd.Env = os.Environ()
		if config.runAsUser != nil && config.runAsUser.userHome != "" {
			for i, v := range cmd.Env {
				if strings.HasPrefix(v, "HOME=") {
					cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
					break
				}
			}
			cmd.Env = append(cmd.Env, "HOME="+config.runAsUser.userHome)
		}
		cmdOut, err := cmd.CombinedOutput()
		endTime = time.Now()
		cmdOutStr := string(cmdOut)

		if err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				// cmd started, but did not return a healthy exit code.
				// runner does not consider this an error.
				err = nil
			} else {
				cmdOutStr = fmt.Sprintf("Error: Failed to run '%s': %s", cmd.String(), err)
			}
		}

		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		programOutput.WriteString(cmdOutStr)

		for _, v := range config.healthyExitCodes {
			if exitCode == v {
				succeeded = true
				shouldPrint = config.outputConfig.alwaysPrint
				triesRemaining = 0
				break
			}
		}

		if !shouldPrint {
			for _, v := range config.outputConfig.printIfMatch {
				if strings.Contains(cmdOutStr, v) {
					shouldPrint = true
					break
				}
			}
		}
		if !shouldPrint {
			for _, v := range config.outputConfig.printIfNotMatch {
				if !strings.Contains(cmdOutStr, v) {
					shouldPrint = true
					break
				}
			}
		}
	}

	if config.workDir == "" {
		var err error
		config.workDir, err = os.Getwd()
		if err != nil {
			config.outputConfig.addSetupWarning(fmt.Sprintf(
				"Failed to get runner's current working directory: %s (this error affects printed output only)", err))
		}
	}

	statusEmoj := "🔴"
	statusStr := statusFailed
	if succeeded {
		statusEmoj = "🟢"
		statusStr = statusSucceeded
	}

	jobSummaryOutput := fmt.Sprintf(
		"[%s] %s running %s\n"+
			"Working directory: %s\n"+
			"Command: %s\n"+
			"Exit code: %d\n\n"+
			"Duration: %s\n"+
			"Start time: %s\n"+
			"End time: %s\n"+
			"Retries allowed: %d\n\n",
		config.outputConfig.hostname,
		statusStr,
		config.outputConfig.jobName,
		config.workDir,
		exec.Command(config.programName, config.programArgs...).String(),
		exitCode,
		endTime.Sub(startTime).String(),
		startTime.Format("2006-01-02 15:04:05.000 -0700"),
		endTime.Format("2006-01-02 15:04:05.000 -0700"),
		config.retries,
	)
	output := strings.Builder{}
	output.WriteString(jobSummaryOutput)
	if config.runAsUser != nil {
		if config.runAsUser.runAsUserName != "" {
			output.WriteString(fmt.Sprintf("Run as user %s:\n", config.runAsUser.runAsUserName))
		} else {
			output.WriteString("Run as:\n")
		}
		output.WriteString(fmt.Sprintf("\tUID: %d\n", config.runAsUser.runAsUID))
		output.WriteString(fmt.Sprintf("\tGID: %d\n\n", config.runAsUser.runAsGID))
	}
	if !config.outputConfig.hideEnv {
		output.WriteString("Environment:\n")
		for _, envVar := range os.Environ() {
			envVarPair := strings.SplitN(envVar, "=", 2)
			envVarName := envVarPair[0]
			if shouldHideEnvVar(envVarName) {
				continue
			}
			output.WriteString(fmt.Sprintf("\t%s=%s\n", envVarName, censoredEnvVarValue(envVarName, envVarPair[1])))
		}
		output.WriteRune('\n')
	}
	if len(config.outputConfig.setupWarnings) > 0 {
		output.WriteString("--- Runner Setup Warnings ---\n\n")
		for _, warningLog := range config.outputConfig.setupWarnings {
			output.WriteString(warningLog)
			output.WriteRune('\n')
		}
		output.WriteRune('\n')
	}
	output.WriteString("--- Program Output ---\n\n")
	if programOutput.Len() == 0 {
		output.WriteString("(no output produced)\n")
	} else {
		output.WriteString(programOutput.String())
	}

	summaryLine := fmt.Sprintf("[%s] %s running %s", config.outputConfig.hostname, statusStr, config.outputConfig.jobName)

	return &runOutput{
		output:      output.String(),
		summaryLine: summaryLine,
		jobName:     config.outputConfig.jobName,
		startTime:   startTime,
		endTime:     endTime,
		shouldPrint: shouldPrint,
		succeeded:   succeeded,
		emoj:        statusEmoj,
	}
}

func (c *runOutputConfig) addSetupWarning(warning string) {
	c.setupWarnings = append(c.setupWarnings, warning)
}
