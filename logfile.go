package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type logConfig struct {
	logDir      string
	logFileName string
	runAsUID    int
	runAsGID    int
}

const (
	defaultLogDirPerm  = 0770
	defaultLogFilePerm = 0660
)

func writeLogs(cfg *logConfig, runOut *runOutput, deliveryErrs []error) error {
	if cfg.logDir == "" {
		return nil
	}

	if _, err := os.Stat(cfg.logDir); os.IsNotExist(err) {
		err = os.MkdirAll(cfg.logDir, defaultLogDirPerm)
		if err != nil {
			return fmt.Errorf("failed to create log directory '%s': %w", cfg.logDir, err)
		}
		if cfg.runAsUID != -1 || cfg.runAsGID != -1 {
			err = os.Chown(cfg.logDir, cfg.runAsUID, cfg.runAsGID)
			if err != nil {
				return fmt.Errorf("failed to chown log directory '%s' (%d, %d): %w", cfg.logDir, cfg.runAsUID, cfg.runAsGID, err)
			}
		}
	}

	logFile := filepath.Join(cfg.logDir, cfg.logFileName)

	logContent := strings.Builder{}
	logContent.WriteString(runOut.output)
	if len(deliveryErrs) > 0 {
		logContent.WriteString("\n--- Runner Delivery Errors ---\n\n")
		for _, err := range deliveryErrs {
			logContent.WriteString(err.Error())
			logContent.WriteRune('\n')
		}
	}

	err := writeLogFile(logFile, logContent.String())
	if err != nil {
		return fmt.Errorf("failed to write log file '%s': %w", logFile, err)
	}

	if cfg.runAsUID != -1 || cfg.runAsGID != -1 {
		err = os.Chown(logFile, cfg.runAsUID, cfg.runAsGID)
		if err != nil {
			return fmt.Errorf("failed to chown log file '%s' (%d, %d): %w", logFile, cfg.runAsUID, cfg.runAsGID, err)
		}
	}

	return nil
}

func writeLogFile(filename, data string) error {
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, defaultLogFilePerm)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data)
	return err
}
