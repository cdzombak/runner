package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/AnthonyHewins/gotfy"
)

type deliveryConfig struct {
	mail    *mailDeliveryConfig
	ntfy    *ntfyDeliveryConfig
	discord *discordDeliveryConfig
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

// discordDeliveryConfig, if provided, is assumed to be complete, valid, and internally consistent.
type discordDeliveryConfig struct {
	discordWebhookURL string
	logFileName       string
}

const ntfyTimeout = 15 * time.Second
const discordTimeout = 15 * time.Second

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
	if config.discord != nil {
		deliveryErrors = extendErrSlice(deliveryErrors,
			executeDiscordDelivery(config.discord, runOutput))
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
		fmt.Sprintf("%s %s", runOutput.emoj, runOutput.summaryLine),
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

func executeDiscordDelivery(cfg *discordDeliveryConfig, runOutput *runOutput) error {
	webhookBody := &bytes.Buffer{}
	writer := multipart.NewWriter(webhookBody)
	err := writer.WriteField("content", fmt.Sprintf("%s %s", runOutput.emoj, runOutput.summaryLine))
	if err != nil {
		return fmt.Errorf("failed building Discord webhook body (.WriteField): %w", err)
	}
	filePart, err := writer.CreateFormFile("files[0]", cfg.logFileName)
	if err != nil {
		return fmt.Errorf("failed building Discord webhook body (.CreateFormFile): %w", err)
	}
	_, err = filePart.Write([]byte(runOutput.output))
	if err != nil {
		return fmt.Errorf("failed attaching log file to Discord webhook body: %w", err)
	}
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("failed building Discord webhook body (.Close): %w", err)
	}

	client := http.DefaultClient
	client.Timeout = discordTimeout

	resp, err := client.Post(cfg.discordWebhookURL, writer.FormDataContentType(), webhookBody)
	if err != nil {
		return fmt.Errorf("failed POSTing Discord webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		respContent, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed POSTing Discord webhook (%s) and reading response body: %w", resp.Status, err)
		}
		return fmt.Errorf("failed POSTing Discord webhook (%s): %s", resp.Status, respContent)
	}
	return nil
}

func extendErrSlice(errs []error, err error) []error {
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}
