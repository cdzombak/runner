package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AnthonyHewins/gotfy"
	mail "github.com/xhit/go-simple-mail/v2"
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

const (
	successNotifyTimeout = 10 * time.Second
	ntfyTimeout          = 10 * time.Second
	discordTimeout       = 10 * time.Second
	mailTimeout          = 10 * time.Second
)

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
	server := mail.NewSMTPClient()
	server.Host = cfg.smtpHost
	server.Port = cfg.smtpPort
	server.Username = cfg.smtpUser
	server.Password = cfg.smtpPassword
	server.KeepAlive = false
	server.ConnectTimeout = mailTimeout
	server.SendTimeout = mailTimeout

	smtpClient, err := server.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

	email := mail.NewMSG()
	email.SetFrom(cfg.mailFrom)
	email.AddTo(cfg.mailTo)
	email.SetSubject(fmt.Sprintf("%s %s", runOutput.emoj, runOutput.summaryLine))
	email.AddHeader("X-Mailer", productIdentifier())
	body := strings.ReplaceAll(runOutput.output, "\n", "\r\n")
	if cfg.tabCharReplacement != "" {
		body = strings.ReplaceAll(body, "\t", cfg.tabCharReplacement)
	}
	email.SetBody(mail.TextPlain, body)
	if email.Error != nil {
		return fmt.Errorf("failed to build email: %w", email.Error)
	}

	if err = email.Send(smtpClient); err != nil {
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

func deliverSuccessNotification(url string) error {
	client := http.DefaultClient
	client.Timeout = successNotifyTimeout
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to build GET request for '%s': %w", url, err)
	}
	req.Header.Set("User-Agent", productIdentifier())
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to GET '%s': %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode > 200 || resp.StatusCode < 299 {
		return fmt.Errorf("failed to GET '%s' (%s)", url, resp.Status)
	}
	return nil
}

func extendErrSlice(errs []error, err error) []error {
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}
