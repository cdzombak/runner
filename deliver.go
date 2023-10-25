package main

import (
	"context"
	"fmt"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/AnthonyHewins/gotfy"
)

type deliveryConfig struct {
	mail *mailDeliveryConfig
	ntfy *ntfyDeliveryConfig
}

// mailDeliveryConfig, if provided, is assumed to be complete, valid, and internally consistent.
type mailDeliveryConfig struct {
	mailTo             string
	mailFrom           string
	smtpUser           string
	smtpPassword       string
	smtpHost           string
	smtpPort           int
	tabCharReplacement string
}

// ntfyDeliveryConfig, if provided, is assumed to be complete, valid, and internally consistent.
type ntfyDeliveryConfig struct {
	ntfyServerURL   *url.URL
	ntfyTopic       string
	ntfyTags        string
	ntfyEmail       string
	ntfyAccessToken string
	ntfyPriority    int
}

const ntfyTimeout = time.Second * 10

func executeDeliveries(config *deliveryConfig, runOutput *runOutput) []error {
	var deliveryErrors []error
	if config.mail != nil {
		deliveryErrors = extendErrSlice(deliveryErrors,
			executeMailDelivery(config.mail, runOutput))
	}
	if config.ntfy != nil {
		deliveryErrors = extendErrSlice(deliveryErrors,
			executeNtfyDelivery(config.ntfy, runOutput))
	}
	return deliveryErrors
}

func executeMailDelivery(cfg *mailDeliveryConfig, runOutput *runOutput) error {
	body := strings.ReplaceAll(runOutput.output, "\n", "\r\n")
	if cfg.tabCharReplacement != "" {
		body = strings.ReplaceAll(body, "\t", cfg.tabCharReplacement)
	}

	msg := []byte(fmt.Sprintf(
		"From: %s\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"X-Mailer: %s\r\n\r\n"+
			"%s\r\n",
		cfg.mailFrom, cfg.mailTo,
		runOutput.summaryLine,
		productIdentifier(),
		body,
	))
	smtpAddr := fmt.Sprintf("%s:%d", cfg.smtpHost, cfg.smtpPort)
	auth := smtp.PlainAuth("", cfg.smtpUser, cfg.smtpPassword, cfg.smtpHost)

	err := smtp.SendMail(smtpAddr, auth, cfg.mailFrom, []string{cfg.mailTo}, msg)
	if err != nil {
		return fmt.Errorf("failed to send email to %s: %w", cfg.mailTo, err)
	}
	return nil
}

func executeNtfyDelivery(cfg *ntfyDeliveryConfig, runOutput *runOutput) error {
	ntfyPublisher, err := gotfy.NewPublisher(nil, cfg.ntfyServerURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create ntfy publisher: %w", err)
	}
	ntfyPublisher.Headers.Set("user-agent", productIdentifier())
	if cfg.ntfyAccessToken != "" {
		ntfyPublisher.Headers.Set("authorization", fmt.Sprintf("Bearer %s", cfg.ntfyAccessToken))
	}

	ctx, cancel := context.WithTimeout(context.Background(), ntfyTimeout)
	defer cancel()
	_, err = ntfyPublisher.SendMessage(ctx, &gotfy.Message{
		Topic:    cfg.ntfyTopic,
		Tags:     strings.Split(cfg.ntfyTags, ","),
		Priority: gotfy.Priority(cfg.ntfyPriority),
		Email:    cfg.ntfyEmail,
		Title:    runOutput.summaryLine,
		Message:  runOutput.output,
	})
	if err != nil {
		return fmt.Errorf("failed to send ntfy notification: %w", err)
	}
	return nil
}

func extendErrSlice(errs []error, err error) []error {
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}
