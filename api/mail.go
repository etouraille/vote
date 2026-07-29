package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

const brevoAPIURL = "https://api.brevo.com/v3/smtp/email"

type brevoContact struct {
	Email string `json:"email"`
}

type brevoEmailRequest struct {
	Sender      brevoContact   `json:"sender"`
	To          []brevoContact `json:"to"`
	Subject     string         `json:"subject"`
	TextContent string         `json:"textContent"`
}

// mailConfigured reports whether Brevo has the two settings it needs. Used
// to decide at startup whether the email notification channel can be
// offered at all (see notify.EmailChannel), rather than registering a
// channel that would fail on every send.
func mailConfigured() bool {
	return os.Getenv("BREVO_API_KEY") != "" && os.Getenv("MAIL_FROM") != ""
}

// sendEmail delivers one plain-text email through Brevo. Kept generic —
// sendValidationEmail below is one caller, the notification email channel
// is another.
func sendEmail(toEmail, subject, textContent string) error {
	apiKey := os.Getenv("BREVO_API_KEY")
	from := os.Getenv("MAIL_FROM")
	if apiKey == "" || from == "" {
		return fmt.Errorf("brevo not configured: BREVO_API_KEY and MAIL_FROM must be set")
	}

	body := brevoEmailRequest{
		Sender:      brevoContact{Email: from},
		To:          []brevoContact{{Email: toEmail}},
		Subject:     subject,
		TextContent: textContent,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, brevoAPIURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("brevo API error: status %d", resp.StatusCode)
	}
	return nil
}

func sendValidationEmail(toEmail string, code int) error {
	frontBaseURL := os.Getenv("FRONT_BASE_URL")
	if frontBaseURL == "" {
		frontBaseURL = "http://localhost:4300"
	}

	confirmLink := fmt.Sprintf("%s/confirm?email=%s&code=%d", frontBaseURL, url.QueryEscape(toEmail), code)

	return sendEmail(toEmail, "Confirmez votre compte", fmt.Sprintf(
		"Bonjour,\n\nCliquez sur ce lien pour valider votre compte :\n%s\n\nCode de validation : %d\n",
		confirmLink, code,
	))
}
