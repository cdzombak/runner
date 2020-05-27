# `runner`

Runs a program, capturing its output and only printing it if the program fails.

Output can also be printed if the program produces, or does not produce, a specific string; and output is optionally written to a log directory regardless of program exit status.

## Use Cases

This is particularly useful when running a program via cron, when you only want to see its output (typically via email) in case of failure or when specific outputs occur.

## Installation

**Requirements:** Go >= 1.13.

`make install` will build `runner` for your current OS/architecture and install it to `/usr/local/bin`.

## Usage

```
[RUNNER_LOG_DIR="$HOME/log/runner"] runner [OPTIONS] -- /path/to/program --program-args
```

### Options

- `-healthy-exit int`: "Healthy" or "success" exit codes. May be specified multiple times to provide more than one success exit code. (default: 0)
- `-hide-env`: Hide the process’s environment, which is normally printed & logged as part of the output.
- `-job-name string`: Job name used in failure notifications and log file name. (default: program name, without path)
- `-log-dir string`: The directory to write run logs to. Can also be set by the `RUNNER_LOG_DIR` environment variable; this flag overrides the environment variable.
- `-print-if-match string`: Print output if the given (case-sensitive) string appears in the program’s output, even if it was a healthy exit. May be specified multiple times.
- `-print-if-not-match string`: Print output if the given (case-sensitive) string does not appear in the program’s output, even if it was a healthy exit. May be specified multiple times.
- `-work-dir string`: Set the working directory for the program. Optional.

### Sample Output

```
Success running exampledatejob
Command: /bin/date
Exit code: 0
Working directory: /home/ubuntu

Duration: 3.794033ms
Start time: 2020-05-27 09:17:59 -0400
End time: 2020-05-27 09:17:59 -0400

--- Program output follows: ---

Wed May 27 09:17:59 EDT 2020
```

## Log Storage

I recommend storing your personal logs in `$HOME/log/runner`. Accomplish this by setting the `RUNNER_LOG_DIR` environment variable at the top of your crontab:

```
RUNNER_LOG_DIR=/home/myusername/log/runner
```

`runner` will create this folder for you if it doesn’t already exist.

### Removing Old Logs

Schedule a cleanup job to run daily via cron:

```
RUNNER_LOG_DIR="/home/myusername/log/runner"
# ...
0	0	*	*	*	/usr/bin/find "$RUNNER_LOG_DIR" -mtime +30 -name "*.log" -delete
```

This will remove logs older than 30 days.

## About

- Issues: https://github.com/cdzombak/runner/issues/new
- Author: [Chris Dzombak](https://www.dzombak.com)
