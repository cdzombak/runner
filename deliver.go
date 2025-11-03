package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cdzombak/gotfy"
	mail "github.com/xhit/go-simple-mail/v2"
)

type deliveryConfig struct {
	mail    *mailDeliveryConfig
	ntfy    *ntfyDeliveryConfig
	discord *discordDeliveryConfig
	slack   *slackDeliveryConfig
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

// slackDeliveryConfig, if provided, is assumed to be complete, valid, and internally consistent.
type slackDeliveryConfig struct {
	slackWebhookURL string
	slackUsername   string
	slackIconEmoji  string
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
	if config.slack != nil {
		deliveryErrors = extendErrSlice(deliveryErrors,
			executeSlackDelivery(config.slack, runOutput))
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

	// TODO(cdzombak): allow configuring mail encryption type
	// https://github.com/cdzombak/runner/issues/11
	switch cfg.smtpPort {
	case 465:
		server.Encryption = mail.EncryptionSSLTLS
	case 587:
		server.Encryption = mail.EncryptionSTARTTLS
	default:
		server.Encryption = mail.EncryptionNone
	}

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
	var ntfyAuth gotfy.Authorization
	if cfg.ntfyAccessToken != "" {
		ntfyAuth = gotfy.AccessToken(cfg.ntfyAccessToken)
	}
	ntfyPublisher := gotfy.NewPublisher(gotfy.PublisherOpts{
		Server: cfg.ntfyServerURL,
		Auth:   ntfyAuth,
		Headers: http.Header{
			"User-Agent": {productIdentifier()},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), ntfyTimeout)
	defer cancel()
	_, err := ntfyPublisher.Send(ctx, gotfy.Message{
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

	req, err := http.NewRequest(http.MethodPost, cfg.discordWebhookURL, webhookBody)
	if err != nil {
		return fmt.Errorf("failed building Discord webhook HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", productIdentifier())

	resp, err := client.Do(req)
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

func executeSlackDelivery(cfg *slackDeliveryConfig, runOutput *runOutput) error {
	client := http.DefaultClient
	client.Timeout = discordTimeout

	payload := map[string]string{
		"text": fmt.Sprintf("%s %s", runOutput.emoj, runOutput.summaryLine),
	}
	if cfg.slackUsername != "" {
		payload["username"] = cfg.slackUsername
	}
	if cfg.slackIconEmoji != "" {
		payload["icon_emoji"] = cfg.slackIconEmoji
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, cfg.slackWebhookURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to build Slack webhook HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", productIdentifier())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed POSTing Slack webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respContent, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed POSTing Slack webhook (%s) and reading response body: %w", resp.Status, err)
		}
		return fmt.Errorf("failed POSTing Slack webhook (%s): %s", resp.Status, respContent)
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
	if resp.StatusCode > 299 || resp.StatusCode < 200 {
		// special case: don't send failures for intermittent Uptime Kuma bug
		// https://github.com/louislam/uptime-kuma/issues/5357 :
		if resp.StatusCode == 404 {
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "ok\":false") && strings.Contains(string(body), "Duplicate entry") {
				return nil
			}
		}

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
