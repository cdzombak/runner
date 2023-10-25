# `runner`: a lightweight wrapper for cron jobs and containers

`runner` runs a program, capturing its output (from both standard output and standard error) and printing it to standard output only if the program fails.

The output can also be printed if the program produces, or does not produce, a specific string (see `-print-if-match` and `-print-if-not-match`). As of Runner 2.0.0, output can always be printed regardless of program exit status, with the `-always-print` option.

If the program failed, or its output would otherwise be printed, `runner` can also:

- email the program's output, if provided with an SMTP server and credentials
- send a notification via [ntfy](https://ntfy.sh)
- send a notification to a Discord webhook

Output is optionally written to a log directory, regardless of program exit status.

If `runner` is run as `root` or with `CAP_SETUID` and `CAP_SETGID`, the target program can be run as a different user.

On Linux 5.6+, if run with `CAP_SYS_PTRACE`, `runner` can redirect its output to file descriptors of your choice belonging to a different process. This is useful in some containerization situations. To use this feature, the container must be run with `--cap-add CAP_SYS_PTRACE`.

## Use Cases

### Cron

The core `runner` logic (capturing and discarding program output) is useful when running a program via cron, when you only want to see its output, typically via email, in case of failure or when specific outputs occur.

### Containers

`runner` 2+ is useful in various containerization applications, even as a container entrypoint. It can run a target program in the container, retry it if need be, capture its output, and then send the output via email, ntfy, or Discord. Email, ntfy, and Discord outputs can be configured by the image's end user via environment variables, requiring no build-time customization. Regardless of these output delivery options, it can always log the program's result to a file as well.

It can even run the target program as a non-root user, and if `runner` is not the container's entrypoint, you can set environment variables like the following to redirect output to the root process's stdout/stderr:

```shell
RUNNER_OUTFD_PID=1
RUNNER_OUTFD_STDOUT=1
RUNNER_OUTFD_STDERR=2
```

## Installation

### Debian via apt repository

Install my Debian repository if you haven't already:

```shell
sudo apt-get install ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dist.cdzombak.net/deb.key | sudo gpg --dearmor -o /etc/apt/keyrings/dist-cdzombak-net.gpg
sudo chmod 0644 /etc/apt/keyrings/dist-cdzombak-net.gpg
echo -e "deb [signed-by=/etc/apt/keyrings/dist-cdzombak-net.gpg] https://dist.cdzombak.net/deb/oss any oss\n" | sudo tee -a /etc/apt/sources.list.d/dist-cdzombak-net.list > /dev/null
sudo apt-get update
```

Then install `runner` via `apt-get`:

```shell
sudo apt-get install runner
```

### Manual installation from build artifacts

Pre-built binaries for Linux and macOS on various architectures are downloadable from each [GitHub Release](https://github.com/cdzombak/runner/releases). Debian packages for each release are available as well.

### Build and install locally

**Requirements:** Go >= 1.15.

`make build` will build `runner` for your current OS/architecture. Copy the resulting binary from `./out/runner` to wherever makes sense for your deployment, and adjust its owner as necessary:

```shell
git clone https://github.com/cdzombak/runner.git
cd runner
make build

cp out/runner $INSTALL_DIR
sudo chown root:root $INSTALL_DIR/runner
sudo chmod 755 $INSTALL_DIR/runner
```

## Post-Installation Steps

If you plan to use the `RUNNER_OUTFD_PID` and `RUNNER_OUTFD_STD[OUT|ERR]` variables, run `setcap 'CAP_SYS_PTRACE=ep' /path/to/runner` on the `runner` binary.

## Usage

```text
[ENV VARS] runner [OPTIONS] -- /path/to/myprogram --myprogram-args
```

### Options

- `-always-print`: Always print the program's output, sidestepping exit code and `-print-if[-not]-match` checks.
- `-healthy-exit value`: "Healthy" or "success" exit codes. May be specified multiple times to provide more than one success exit code. (default: `0`)
- `-hide-env`: Hide the process's environment, which is normally printed & logged as part of the output.
- `-job-name string`: Job name used in failure notifications and log file name. (default: program name, without path)
- `-log-dir string`: The directory to write run logs to.
  - Can also be set by the `RUNNER_LOG_DIR` environment variable; this flag overrides the environment variable.
- `-print-if-match value`: Print/mail output if the given (**case-sensitive**) string appears in the program's output, even if it was a healthy exit. May be specified multiple times.
- `-print-if-not-match value`: Print/mail output if the given (**case-sensitive**) string does not appear in the program's output, even if it was a healthy exit. May be specified multiple times.
- `-retries int`: If the command fails, retry it this many times. (default: `0`)
- `-retry-delay int`: If the command fails, wait this many seconds before retrying. (default: `0`)
- `-version`: Print version and exit.
- `-work-dir string`: Set the working directory for the program.

#### Hiding sensitive environment variables

- `RUNNER_CENSOR_ENV` (environment variable only): Colon-separated list of environment variables whose values will be censored in output. `RUNNER_SMTP_PASS` and `RUNNER_NTFY_ACCESS_TOKEN` are always censored.
- `RUNNER_HIDE_ENV` (environment variable only): Colon-separated list of environment variables which will be entirely omitted from output.

#### Run as another user

- `-gid int`: Run the program as the given GID. Ignored on Windows. (If provided, runner must be run as `root` or with `CAP_SETGID`.)
- `-uid int`: Run the program as the given UID. Ignored on Windows. (If provided, runner must be run as `root` or with `CAP_SETUID`.)
- `-user string`: Run the program as the given user. Ignored on Windows. (If provided, runner must be run as `root` or with `CAP_SETUID` and `CAP_SETGID`.)

#### Email options

- `-mail-from string`: The email address to use as the `From:` address in failure emails. (default: `runner@hostname`)
  - Can also be set by the `RUNNER_MAIL_FROM` environment variable; this flag overrides the environment variable.
- `-mail-tab-char string`: Replace tab characters in emailed output by this string.
  - Can also be set by the `RUNNER_MAIL_TAB_CHAR` environment variable; this flag overrides the environment variable.
- `-mailto string`: Send an email to the given address if the program fails or its output would otherwise be printed per `-healthy-exit`/`-print-if-[not]-match`/`-always-print`.
  - Can also be set by the `RUNNER_MAILTO` environment variable; this flag overrides the environment variable.
- `-smtp-host string`: SMTP server hostname.
  - Can also be set by the `RUNNER_SMTP_HOST` environment variable; this flag overrides the environment variable.
- `-smtp-pass string`: Password for SMTP authentication.
  - Can also be set by the `RUNNER_SMTP_PASS` environment variable; this flag overrides the environment variable.
- `-smtp-port int`: SMTP server port.
  - Can also be set by the `RUNNER_SMTP_PORT` environment variable; this flag overrides the environment variable. (default: 25)
- `-smtp-user string`: Username for SMTP authentication.
  - Can also be set by the `RUNNER_SMTP_USER` environment variable; this flag overrides the environment variable.

#### Ntfy options

- `-ntfy-access-token string`: If set, use this access token for ntfy.
  - Can also be set by the `RUNNER_NTFY_ACCESS_TOKEN` environment variable; this flag overrides the environment variable.
- `-ntfy-email string`: If set, tell ntfy to send an email to this address.
  - Can also be set by the `RUNNER_NTFY_EMAIL` environment variable; this flag overrides the environment variable.
- `-ntfy-priority int`: Priority for the notification sent to ntfy. Must be between 1-5, inclusive.
  - Can also be set by the `RUNNER_NTFY_PRIORITY` environment variable; this flag overrides the environment variable. (default 3)
- `-ntfy-server string`: Send a notification to the given ntfy server if the program fails or its output would otherwise be printed per -healthy-exit/-print-if-[not]-match/-always-print.
  - Can also be set by the `RUNNER_NTFY_SERVER` environment variable; this flag overrides the environment variable.
- `-ntfy-tags string`: Comma-separated list of ntfy tags to send.
  - Can also be set by the `RUNNER_NTFY_TAGS` environment variable; this flag overrides the environment variable.
- `-ntfy-topic string`: The ntfy topic to send to.
  - Can also be set by the `RUNNER_NTFY_TOPIC` environment variable; this flag overrides the environment variable.

#### Discord options

- `-discord-webhook string`: If set, post to this Discord webhook if the program fails or its output would otherwise be printed per -healthy-exit/-print-if-[not]-match/-always-print.
  - Can also be set by the `RUNNER_DISCORD_WEBHOOK` environment variable; this flag overrides the environment variable.

### Sample Output

```text
[myhostname] Success running exampledatejob
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

I store my personal logs in `$HOME/log/runner`. Accomplish this by setting the `RUNNER_LOG_DIR` environment variable at the top of your crontab:

```shell
RUNNER_LOG_DIR=/home/myusername/log/runner
```

`runner` will create this folder for you if it doesn’t already exist.

### Removing Old Logs

Schedule a cleanup job to run daily via cron:

```text
RUNNER_LOG_DIR="/home/myusername/log/runner"
# ...
0   0   *   *   *   /usr/bin/find "$RUNNER_LOG_DIR" -mtime +30 -name "*.log" -delete
```

This will remove logs older than 30 days.

## About

- [Issue Tracker](https://github.com/cdzombak/runner/issues)
- Author: [Chris Dzombak](https://www.dzombak.com)
  - [GitHub: @cdzombak](https://www.github.com/cdzombak)

## License

LGPLv3; see `LICENSE` in this repository.
